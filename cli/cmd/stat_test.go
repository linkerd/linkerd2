package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSortStatsKeys(t *testing.T) {
	unsorted := map[string]*row{
		"kube-system/heapster-v1.4.3":      {0.008091, 24.137931, 516666, 990333},
		"test/backend4":                    {0.066121, 38.818565, 494553, 989891},
		"test/hello10":                     {0.000000, 0.000000, 0, 0},
		"test/world-deploy1":               {0.051893, 33.870968, 510526, 990210},
		"test/world-deploy2":               {2.504800, 33.749165, 497249, 989944},
		"kube-system/kubernetes-dashboard": {0.017856, 39.062500, 520000, 990400},
		"other/grafana":                    {0.060557, 35.944212, 518960, 990379},
		"kube-system/l7-default-backend":   {0.020371, 31.508049, 516923, 990338},
	}

	expected := []string{"other/grafana", "kube-system/heapster-v1.4.3", "kube-system/kubernetes-dashboard",
		"kube-system/l7-default-backend", "test/backend4", "test/hello10", "test/world-deploy1", "test/world-deploy2"}

	sorted := sortStatsKeys(unsorted)
	assert.Equal(t, expected, sorted, "Not Sorted!")
}
