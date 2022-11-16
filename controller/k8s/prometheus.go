package k8s

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/client-go/tools/cache"
)

type promGauges struct {
	gauges []prometheus.GaugeFunc
}

func (p *promGauges) addInformerSize(kind string, inf cache.SharedIndexInformer) {
	p.gauges = append(p.gauges, prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Name: fmt.Sprintf("%s_cache_size", kind),
		Help: fmt.Sprintf("Number of items in the client-go %s cache", kind),
	}, func() float64 {
		return float64(len(inf.GetStore().ListKeys()))
	}))
}
