/*

Package testutil provides helpers for running the linkerd integration tests.

All helpers are defined as functions on the TestHelper struct, which you should
instantiate once per test, using the NewTestHelper function. Since that function
also parses command line flags, it should be called as part of your test's
TestMain function. For example:

	package mytest

	import (
		"os"
		"testing"

		"github.com/linkerd/linkerd2/testutil"
	)

	var TestHelper *util.TestHelper

	func TestMain(m *testing.M) {
	  TestHelper = util.NewTestHelper()
	  os.Exit(m.Run())
	}

	func TestMyTest(t *testing.T) {
		// add test code here
	}

Calling NewTestHelper adds the following command line flags:

	-linkerd string
		path to the linkerd binary to test
	-linkerd-namespace string
		the namespace where linkerd is installed (default "linkerd")
	-k8s-context string
		the kubernetes context associated with the test cluster (default "")
	-integration-tests
		must be provided to run the integration tests

Note that the -integration-tests flag must be set when running tests, so that
the tests aren't inadvertently executed when unit tests for the project are run.

TestHelper embeds KubernetesHelper, so all functions defined on KubernetesHelper
are also available to instances of TestHelper. See the individual function
definitions for details on how to use each helper in tests.

*/
package testutil
