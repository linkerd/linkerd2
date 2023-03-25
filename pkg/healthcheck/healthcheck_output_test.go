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
	fakeGlob := func(path string) ([]string, error) {
		dir, _ := filepath.Split(path)
		return []string{
			filepath.Join(dir, "linkerd-bar"),
			filepath.Join(dir, "linkerd-baz"),
			filepath.Join(dir, "linkerd-foo"),
		}, nil
	}

	fcmd := fakeexec.FakeCmd{
		RunScript: []fakeexec.FakeAction{
			func() ([]byte, []byte, error) {
				return []byte(`{"name":"linkerd-baz","checks":"always"}`), nil, nil
			},
			func() ([]byte, []byte, error) {
				return []byte(`{"name":"linkerd-foo-no-match","checks":"always"}`), nil, nil
			},
			func() ([]byte, []byte, error) { return []byte(`{"name":"linkerd-bar","checks":"always"}`), nil, nil },
		},
	}

	lookPathSuccess := false

	fexec := &fakeexec.FakeExec{
		CommandScript: []fakeexec.FakeCommandAction{
			func(cmd string, args ...string) exec.Cmd { return fakeexec.InitFakeCmd(&fcmd, cmd, args...) },
			func(cmd string, args ...string) exec.Cmd { return fakeexec.InitFakeCmd(&fcmd, cmd, args...) },
			func(cmd string, args ...string) exec.Cmd { return fakeexec.InitFakeCmd(&fcmd, cmd, args...) },
		},
		LookPathFunc: func(cmd string) (string, error) {
			if lookPathSuccess {
				return cmd, nil
			}
			lookPathSuccess = true
			return "", errors.New("fake-error")
		},
	}

	extensions, missing := FindExtensions("/path1:/this/is/a/fake/path2", fakeGlob, fexec, []string{"foo", "missing-cli"})

	expExtensions := []Extension{
		{path: "/this/is/a/fake/path2/linkerd-bar"},
		{path: "/path1/linkerd-baz"},
		{path: "/path1/linkerd-foo"},
	}
	expMissing := []string{"linkerd-missing-cli"}

	if !reflect.DeepEqual(expExtensions, extensions) {
		t.Errorf("Expected [%+v] Got [%+v]", expExtensions, extensions)
	}
	if !reflect.DeepEqual(expMissing, missing) {
		t.Errorf("Expected [%+v] Got [%+v]", expMissing, missing)
	}
}
