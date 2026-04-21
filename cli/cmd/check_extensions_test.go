package cmd

import (
	"bytes"
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"k8s.io/utils/exec"
	fakeexec "k8s.io/utils/exec/testing"
)

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

	extensions, missing := findExtensions("/path1:/this/is/a/fake/path2", fakeGlob, fexec, []string{"foo", "missing-cli"})

	expExtensions := []extension{
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

func TestRunExtensionsChecks(t *testing.T) {
	successJSON := `
	{
		"success": true,
		"categories": [
			{
				"categoryName": "success check name",
				"checks": [
					{
						"description": "success check desc",
						"result": "success"
					}
				]
			}
		]
	}
	`

	warningJSON := `
	{
		"success": true,
		"categories": [
			{
				"categoryName": "warning check name",
				"checks": [
					{
						"description": "warning check desc",
						"hint": "https://example.com/warning",
						"error": "this is the warning message",
						"result": "warning"
					}
				]
			}
		]
	}
	`

	errorJSON := `
	{
		"success": false,
		"categories": [
			{
				"categoryName": "error check name",
				"checks": [
					{
						"description": "error check desc",
						"hint": "https://example.com/error",
						"error": "this is the error message",
						"result": "error"
					}
				]
			}
		]
	}
	`

	multiJSON := `
	{
		"success": true,
		"categories": [
			{
				"categoryName": "multi check name",
				"checks": [
					{
						"description": "multi check desc success",
						"result": "success"
					},
					{
						"description": "multi check desc warning",
						"hint": "https://example.com/multi",
						"error": "this is the multi warning message",
						"result": "warning"
					}
				]
			}
		]
	}
	`

	testCases := []struct {
		name        string
		extensions  []extension
		missing     []string
		fakeActions []fakeexec.FakeAction
		expSuccess  bool
		expWarning  bool
		expOutput   string
	}{
		{
			"no checks",
			nil,
			nil,
			[]fakeexec.FakeAction{
				func() ([]byte, []byte, error) {
					return nil, nil, nil
				},
			},
			true,
			false,
			"",
		},
		{
			"invalid JSON",
			[]extension{{path: "/path/linkerd-invalid"}},
			nil,
			[]fakeexec.FakeAction{
				func() ([]byte, []byte, error) {
					return []byte("bad json"), nil, nil
				},
			},
			false,
			false,
			`linkerd-invalid
---------------
× Running: /path/linkerd-invalid check
    invalid extension check output from "/path/linkerd-invalid check" (JSON object expected):
bad json
[invalid character 'b' looking for beginning of value]
    see https://linkerd.io/2/checks/#extensions for hints

`,
		},
		{
			"one successful check",
			[]extension{{path: "/path/linkerd-success"}},
			nil,
			[]fakeexec.FakeAction{
				func() ([]byte, []byte, error) {
					return []byte(successJSON), nil, nil
				},
			},
			true,
			false,
			`success check name
------------------
√ success check desc

`,
		},
		{
			"one warning check",
			[]extension{{path: "/path/linkerd-warning"}},
			nil,
			[]fakeexec.FakeAction{
				func() ([]byte, []byte, error) {
					return []byte(warningJSON), nil, nil
				},
			},
			true,
			true,
			`warning check name
------------------
‼ warning check desc
    this is the warning message
    see https://example.com/warning for hints

`,
		},
		{
			"one error check",
			[]extension{{path: "/path/linkerd-error"}},
			nil,
			[]fakeexec.FakeAction{
				func() ([]byte, []byte, error) {
					return []byte(errorJSON), nil, nil
				},
			},
			false,
			false,
			`error check name
----------------
× error check desc
    this is the error message
    see https://example.com/error for hints

`,
		},
		{
			"one missing check",
			nil,
			[]string{"missing"},
			nil,
			true,
			true,
			`missing
-------
‼ Linkerd extension command missing exists
    exec: "missing": executable file not found in $PATH
    see https://linkerd.io/2/checks/#extensions for hints

`,
		},
		{
			"multiple checks with success, warnings, errors, and missing",
			[]extension{{path: "/path/linkerd-success"}, {path: "/path/linkerd-warning"}, {path: "/path/linkerd-error"}, {path: "/path/linkerd-multi"}},
			[]string{"missing1", "missing2"},
			[]fakeexec.FakeAction{
				func() ([]byte, []byte, error) {
					return []byte(successJSON), nil, nil
				},
				func() ([]byte, []byte, error) {
					return []byte(warningJSON), nil, nil
				},
				func() ([]byte, []byte, error) {
					return []byte(errorJSON), nil, nil
				},
				func() ([]byte, []byte, error) {
					return []byte(multiJSON), nil, nil
				},
			},
			false,
			true,
			`success check name
------------------
√ success check desc

warning check name
------------------
‼ warning check desc
    this is the warning message
    see https://example.com/warning for hints

error check name
----------------
× error check desc
    this is the error message
    see https://example.com/error for hints

multi check name
----------------
√ multi check desc success
‼ multi check desc warning
    this is the multi warning message
    see https://example.com/multi for hints

missing1
--------
‼ Linkerd extension command missing1 exists
    exec: "missing1": executable file not found in $PATH
    see https://linkerd.io/2/checks/#extensions for hints

missing2
--------
‼ Linkerd extension command missing2 exists
    exec: "missing2": executable file not found in $PATH
    see https://linkerd.io/2/checks/#extensions for hints

`,
		},
	}

	for _, tc := range testCases {
		tc := tc // pin
		t.Run(tc.name, func(t *testing.T) {
			fcmd := fakeexec.FakeCmd{
				RunScript: tc.fakeActions,
			}

			fakeCommandActions := make([]fakeexec.FakeCommandAction, len(tc.fakeActions))
			for i := 0; i < len(tc.fakeActions); i++ {
				fakeCommandActions[i] = func(cmd string, args ...string) exec.Cmd { return fakeexec.InitFakeCmd(&fcmd, cmd, args...) }
			}
			fexec := &fakeexec.FakeExec{
				CommandScript: fakeCommandActions,
			}

			var stdout, stderr bytes.Buffer
			success, warning := runExtensionsChecks(&stdout, &stderr, tc.extensions, tc.missing, fexec, nil, "")
			if tc.expSuccess != success {
				t.Errorf("Expected success to be %t, got %t", tc.expSuccess, success)
			}
			if tc.expWarning != warning {
				t.Errorf("Expected warning to be %t, got %t", tc.expWarning, warning)
			}
			output := stdout.String()
			if tc.expOutput != output {
				t.Errorf("Expected output to be:\n%s\nGot:\n%s", tc.expOutput, output)
			}
		})
	}
}

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
