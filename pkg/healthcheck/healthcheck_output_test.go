package healthcheck

import (
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"k8s.io/utils/exec"
	fakeexec "k8s.io/utils/exec/testing"
)

func TestSuffix(t *testing.T) {
	testCases := []*struct {
		testName string
		input    string
		exp      string
	}{
		{
			"empty",
			"",
			"",
		},
		{
			"no path",
			"linkerd-foo",
			"foo",
		},
		{
			"extra dash",
			"linkerd-foo-bar",
			"foo-bar",
		},
		{
			"with path",
			"/tmp/linkerd-foo",
			"foo",
		},
	}
	for _, tc := range testCases {
		tc := tc // pin
		t.Run(tc.testName, func(t *testing.T) {
			result := suffix(tc.input)
			if !reflect.DeepEqual(tc.exp, result) {
				t.Fatalf("Expected [%s] Got [%s]", tc.exp, result)
			}
		})
	}
}

func TestFindExtensions(t *testing.T) {
	paths := []string{
		filepath.Join("/path1", "linkerd-bar"),
		filepath.Join("/path1", "linkerd-baz"),
		filepath.Join("/path1", "linkerd-foo"),
		filepath.Join("/this/is/a/fake/path2", "linkerd-bar"),
		filepath.Join("/this/is/a/fake/path2", "linkerd-baz"),
		filepath.Join("/this/is/a/fake/path2", "linkerd-foo"),
	}

	fcmd := fakeexec.FakeCmd{
		RunScript: []fakeexec.FakeAction{
			func() ([]byte, []byte, error) {
				return []byte(`{"name":"linkerd-bar","checks":"always"}`), nil, errors.New("fake-error")
			},
			func() ([]byte, []byte, error) { return []byte(`{"name":"linkerd-baz","checks":"always"}`), nil, nil },
			func() ([]byte, []byte, error) {
				return []byte(`{"name":"linkerd-foo-no-match","checks":"always"}`), nil, nil
			},
			func() ([]byte, []byte, error) { return []byte(`{"name":"linkerd-bar","checks":"always"}`), nil, nil },
			func() ([]byte, []byte, error) { return []byte(`{"name":"linkerd-foo","checks":"always"}`), nil, nil },
			func() ([]byte, []byte, error) { return []byte(`{"name":"linkerd-foo","checks":"always"}`), nil, nil },
		},
	}

	fexec := &fakeexec.FakeExec{
		CommandScript: []fakeexec.FakeCommandAction{
			func(cmd string, args ...string) exec.Cmd { return fakeexec.InitFakeCmd(&fcmd, cmd, args...) },
			func(cmd string, args ...string) exec.Cmd { return fakeexec.InitFakeCmd(&fcmd, cmd, args...) },
			func(cmd string, args ...string) exec.Cmd { return fakeexec.InitFakeCmd(&fcmd, cmd, args...) },
			func(cmd string, args ...string) exec.Cmd { return fakeexec.InitFakeCmd(&fcmd, cmd, args...) },
			func(cmd string, args ...string) exec.Cmd { return fakeexec.InitFakeCmd(&fcmd, cmd, args...) },
			func(cmd string, args ...string) exec.Cmd { return fakeexec.InitFakeCmd(&fcmd, cmd, args...) },
		},
		LookPathFunc: func(cmd string) (string, error) { return cmd, nil },
	}

	extensions := ExtensionChecks(paths, fexec, nil)

	expExtensions := []string{
		"/this/is/a/fake/path2/linkerd-bar",
		"/path1/linkerd-baz",
		"/this/is/a/fake/path2/linkerd-foo",
	}

	if !reflect.DeepEqual(expExtensions, extensions) {
		t.Errorf("Expected [%+v] Got [%+v]", expExtensions, extensions)
	}
}
