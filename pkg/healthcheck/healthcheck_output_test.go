package healthcheck

import (
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"k8s.io/utils/exec"
	fakeexec "k8s.io/utils/exec/testing"
)

func TestSuffixes(t *testing.T) {
	testCases := []*struct {
		testName  string
		cliChecks CLIChecks
		exp       map[string]struct{}
	}{
		{
			"empty",
			CLIChecks{},
			map[string]struct{}{},
		},
		{
			"empty name",
			CLIChecks{
				CheckCLIOutput{Name: ""}: "",
			},
			map[string]struct{}{
				"": {},
			},
		},
		{
			"one check",
			CLIChecks{
				CheckCLIOutput{Name: "linkerd-foo"}: "filepath",
			},
			map[string]struct{}{
				"foo": {},
			},
		},
		{
			"two checks",
			CLIChecks{
				CheckCLIOutput{Name: "linkerd-foo"}:    "filepath",
				CheckCLIOutput{Name: "linker-bar-baz"}: "filepath",
			},
			map[string]struct{}{
				"foo": {},
				"baz": {},
			},
		},
	}
	for _, tc := range testCases {
		tc := tc // pin
		t.Run(tc.testName, func(t *testing.T) {
			result := tc.cliChecks.Suffixes()
			if !reflect.DeepEqual(tc.exp, result) {
				t.Fatalf("Expected [%+v] Got [%+v]", tc.exp, result)
			}
		})
	}
}

func TestGetCLIChecks(t *testing.T) {
	fakeGlob := func(path string) ([]string, error) {
		dir, _ := filepath.Split(path)
		return []string{
			filepath.Join(dir, "linkerd-foo"),
			filepath.Join(dir, "linkerd-bar"),
			filepath.Join(dir, "linkerd-baz"),
		}, nil
	}

	fcmd := fakeexec.FakeCmd{
		RunScript: []fakeexec.FakeAction{
			func() ([]byte, []byte, error) { return []byte(`{"name":"linkerd-foo-no-match"}`), nil, nil },
			func() ([]byte, []byte, error) {
				return []byte(`{"name":"linkerd-bar"}`), nil, errors.New("fake-error")
			},
			func() ([]byte, []byte, error) { return []byte(`{"name":"linkerd-baz"}`), nil, nil },

			func() ([]byte, []byte, error) { return []byte(`{"name":"linkerd-foo"}`), nil, nil },
			func() ([]byte, []byte, error) { return []byte(`{"name":"linkerd-bar"}`), nil, nil },
			func() ([]byte, []byte, error) { return []byte(`{"name":"linkerd-baz"}`), nil, nil },
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

	cliChecks := GetCLIChecks("/path1:/this/is/a/fake/path2", fakeGlob, fexec)

	exp := CLIChecks{
		CheckCLIOutput{Name: "linkerd-baz"}: "/path1/linkerd-baz",
		CheckCLIOutput{Name: "linkerd-bar"}: "/this/is/a/fake/path2/linkerd-bar",
		CheckCLIOutput{Name: "linkerd-foo"}: "/this/is/a/fake/path2/linkerd-foo",
	}

	if !reflect.DeepEqual(exp, cliChecks) {
		t.Errorf("Expected [%+v] Got [%+v]", exp, cliChecks)
	}
}
