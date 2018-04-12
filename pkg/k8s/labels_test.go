package k8s

import (
	"reflect"
	"testing"

	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestGetOwnerLabels(t *testing.T) {
	t.Run("Maps proxy labels to prometheus labels", func(t *testing.T) {
		metadata := meta.ObjectMeta{
			Labels: map[string]string{
				ProxyDeploymentLabel:            "test-deployment",
				ProxyReplicationControllerLabel: "test-replication-controller",
				ProxyReplicaSetLabel:            "test-replica-set",
				ProxyJobLabel:                   "test-job",
				ProxyDaemonSetLabel:             "test-daemon-set",
			},
		}

		expectedLabels := map[string]string{
			"deployment":             "test-deployment",
			"replication_controller": "test-replication-controller",
			"replica_set":            "test-replica-set",
			"k8s_job":                "test-job",
			"daemon_set":             "test-daemon-set",
		}

		ownerLabels := GetOwnerLabels(metadata)

		if !reflect.DeepEqual(ownerLabels, expectedLabels) {
			t.Fatalf("Expected owner labels [%v] but got [%v]", expectedLabels, ownerLabels)
		}
	})

	t.Run("Ignores non-proxy labels", func(t *testing.T) {
		metadata := meta.ObjectMeta{
			Labels: map[string]string{"app": "foo"},
		}

		ownerLabels := GetOwnerLabels(metadata)

		if len(ownerLabels) != 0 {
			t.Fatalf("Expected no owner labels but got [%v]", ownerLabels)
		}
	})
}
