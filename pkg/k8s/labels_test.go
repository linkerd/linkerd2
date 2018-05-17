package k8s

import (
	"reflect"
	"testing"

	k8sV1 "k8s.io/api/apps/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestGetOwnerLabels(t *testing.T) {
	t.Run("Maps proxy labels to prometheus labels", func(t *testing.T) {
		metadata := meta.ObjectMeta{
			Labels: map[string]string{
				ControllerNSLabel:                     "conduit-namespace",
				ProxyDeploymentLabel:                  "test-deployment",
				ProxyReplicationControllerLabel:       "test-replication-controller",
				ProxyReplicaSetLabel:                  "test-replica-set",
				ProxyJobLabel:                         "test-job",
				ProxyDaemonSetLabel:                   "test-daemon-set",
				ProxyStatefulSetLabel:                 "test-stateful-set",
				k8sV1.DefaultDeploymentUniqueLabelKey: "test-pth",
			},
		}

		expectedLabels := map[string]string{
			"conduit_io_control_plane_ns": "conduit-namespace",
			"deployment":                  "test-deployment",
			"replication_controller":      "test-replication-controller",
			"replica_set":                 "test-replica-set",
			"k8s_job":                     "test-job",
			"daemon_set":                  "test-daemon-set",
			"stateful_set":                "test-stateful-set",
			"pod_template_hash":           "test-pth",
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
