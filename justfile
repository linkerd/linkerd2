
# Print all (transitive) dependencies of the specified go package.
go-mod-tree root='github.com/linkerd/linkerd2':
    #!/usr/bin/env bash
    set -eu
    declare -r GRAPH=$(go mod graph)
    deps_of() {
        local pkg=$1
        echo "$GRAPH" | awk '$1 == "'"$pkg"'" { print $2 }' | sort | uniq
    }
    declare -A PKGS=()
    subtree() {
        local pkg=$1
        local namepfx=${2:-}
        local pfx=${3:-}
        # If the package has already been printed, then print it with an
        # asterisk and skip printing its dependencies
        if (( ${PKGS[$pkg]:-0} )); then
            echo "$namepfx$pkg (*)"
        else
            # Otherwise, print the package, mark it as printed, and the
            # dependency tree for each of its depndencies
            echo "$namepfx$pkg"
            PKGS[$pkg]=1
            local dep=''
            for d in $(deps_of "$pkg") ; do
                if [ -n "$dep" ]; then  subtree "$dep" "$pfx├── " "$pfx│   " ; fi
                dep=$d
            done
            if [ -n "$dep" ]; then subtree "$dep" "$pfx└── " "$pfx    " ; fi
        fi
    }
    if [[ '{{ root }}' == *@* ]]; then
        # The root specifies an exact version, so print its dependencies.
        subtree '{{ root }}'
    elif [ -n "$(echo "$GRAPH" | awk '$1 == "{{ root }}" { print $0 }' | head -n 1)" ]; then
        # The root does not specify an exact version, but that is how the
        # package is listed in go.mod.
        subtree '{{ root }}'
    else
        # The root does not specify an exact version, find all versions of the
        # package and print their depdencies.
        first=1
        for pkg in $(echo "$GRAPH" | awk '{ print $1 }' | sort | uniq) ; do
            if [[ "$pkg" == '{{ root }}'@* ]]; then
                if (( $first )); then first=0; else echo; fi
                subtree "$pkg"
            fi
        done
    fi

# Print all (transitive) dependenents of the specified go package.
go-mod-why dep:
    #!/usr/bin/env bash
    set -eu
    declare -r GRAPH=$(go mod graph)
    depends_on() {
        local pkg=$1
        echo "$GRAPH" | awk '$2 == "'"$pkg"'" { print $1 }' | sort | uniq
    }
    declare -A PKGS=()
    supertree() {
        local pkg=$1
        local namepfx=${2:-}
        local pfx=${3:-}
        if (( ${PKGS[$pkg]:-0} )); then
            echo "$namepfx$pkg (*)"
        else
            PKGS[$pkg]=1
            echo "$namepfx$pkg"
            local parent=''
            for p in $(depends_on "$pkg") ; do
                if [ -n "$parent" ]; then supertree "$parent" "$pfx├── " "$pfx│   " ; fi
                parent=$p
            done
            if [ -n "$parent" ]; then supertree "$parent" "$pfx└── " "$pfx    " ; fi
        fi
    }
    if [[ '{{ dep }}' == *@* ]]; then
        # The dependency specifies an exact version, so print the packages that
        # depend on it.
        supertree '{{ dep }}'
    else
        # The dependency does not specify an exact version, find all versions of
        # the package and print the packages that depend on each of them.
        first=1
        for pkg in $(echo "$GRAPH" | awk '{ print $1 }' | sort | uniq) ; do
            if [[ "$pkg" == '{{ dep }}'@* ]]; then
                if (( $first )); then first=0; else echo; fi
                supertree "$pkg"
            fi
        done
    fi

# Print all versions of the specified go package.
go-mod-versions dep:
    #!/usr/bin/env bash
    set -eu
    if [[ '{{ dep }}' == *@* ]]; then
        echo 'The dependency must not specify an exact version.' >&2
        exit 1
    fi
    for pkg in $(go mod graph | awk '{ print $1; print $2 }' | sort | uniq) ; do
        if [[ "$pkg" == '{{ dep }}'@* ]]; then
            echo "$pkg"
        fi
    done

# vim: set ft=make :
