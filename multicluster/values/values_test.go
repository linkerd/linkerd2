package values

import (
	"github.com/linkerd/linkerd2/testutil"
	"testing"
)

func TestValuesFileInSyncWithGoSource(t *testing.T) {
	var testDataDiffer = testutil.NewTestDataDiffer()
	testDataDiffer.DiffTestFileHashes(t, "values.go", "../charts/linkerd-multicluster/values.yaml", "values_hashes.golden")
}
