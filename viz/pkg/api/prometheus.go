package api

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/linkerd/linkerd2/pkg/k8s"
	metricsApi "github.com/linkerd/linkerd2/viz/metrics-api"
	pb "github.com/linkerd/linkerd2/viz/metrics-api/gen/viz"
	promApi "github.com/prometheus/client_golang/api"
	promv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	promModel "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
	log "github.com/sirupsen/logrus"
)

func NewExternalPrometheusClient(ctx context.Context, kubeAPI *k8s.KubernetesAPI) (PrometheusMetrics, error) {
	portforward, err := k8s.NewPortForward(
		ctx,
		kubeAPI,
		"linkerd-viz",
		"prometheus",
		"localhost",
		0,
		9090,
		false,
	)
	if err != nil {
		return PrometheusMetrics{nil}, err
	}

	addr := fmt.Sprintf("http://%s", portforward.AddressAndPort())
	if err = portforward.Init(); err != nil {
		return PrometheusMetrics{nil}, err
	}

	promClient, err := promApi.NewClient(promApi.Config{Address: addr})
	if err != nil {
		return PrometheusMetrics{nil}, err
	}

	return PrometheusMetrics{promv1.NewAPI(promClient)}, nil
}

type MetricsProvider interface {
	QueryRate(
		ctx context.Context,
		metric string,
		timeWindow string,
		labels model.LabelSet,
		groupBy model.LabelNames,
		resource *pb.Resource,
		results chan<- *model.Sample,
	) error

	QueryQuantile(
		ctx context.Context,
		quantile float64,
		metric string,
		timeWindow string,
		labels model.LabelSet,
		groupBy model.LabelNames,
		resource *pb.Resource,
		results chan<- *model.Sample,
	) error
}

type PrometheusMetrics struct {
	api promv1.API
}

func (p PrometheusMetrics) QueryRate(
	ctx context.Context,
	metric string,
	timeWindow string,
	labels model.LabelSet,
	groupBy model.LabelNames,
	resource *pb.Resource,
	results chan<- *model.Sample,
) error {
	defer close(results)
	query := fmt.Sprintf("sum(increase(%s%s[%s])) by (%s)",
		metric,
		labels.Merge(metricsApi.PromQueryLabels(resource)),
		timeWindow,
		append(groupBy, metricsApi.PromGroupByLabelNames(resource)...),
	)
	val, warn, err := p.api.Query(ctx, query, time.Time{})
	if warn != nil {
		log.Warnf("%v", warn)
	}
	if err != nil {
		return err
	}
	for _, sample := range val.(model.Vector) {
		results <- sample
	}
	return nil
}

func (p PrometheusMetrics) QueryQuantile(
	ctx context.Context,
	quantile float64,
	metric string,
	timeWindow string,
	labels model.LabelSet,
	groupBy model.LabelNames,
	resource *pb.Resource,
	results chan<- *model.Sample,
) error {
	defer close(results)
	query := fmt.Sprintf("histogram_quantile(%.2f, sum(irate(%s%s[%s])) by (le, %s))",
		quantile,
		metric,
		labels.Merge(metricsApi.PromQueryLabels(resource)),
		timeWindow,
		append(groupBy, metricsApi.PromGroupByLabelNames(resource)...),
	)
	val, warn, err := p.api.Query(ctx, query, time.Time{})
	if warn != nil {
		log.Warnf("%v", warn)
	}
	if err != nil {
		return err
	}
	for _, sample := range val.(model.Vector) {
		results <- sample
	}
	return nil
}

type ProxyMetrics struct {
	api *k8s.KubernetesAPI
}

func NewProxyMetrics(k8sAPI *k8s.KubernetesAPI) ProxyMetrics {
	return ProxyMetrics{k8sAPI}
}

func (p ProxyMetrics) QueryRate(
	ctx context.Context,
	metric string,
	timeWindow string,
	labels model.LabelSet,
	groupBy model.LabelNames,
	resource *pb.Resource,
	results chan<- *model.Sample,
) error {
	defer close(results)
	pods, err := k8s.GetPodsFor(ctx, p.api, resource.Namespace, fmt.Sprintf("%s/%s", resource.Type, resource.Name))
	if err != nil {
		return err
	}

	initalValues := make(map[string]float64)

	podsMetrics := k8s.GetMetrics(p.api, pods, k8s.ProxyAdminPortName, 30*time.Second, false)
	for _, podMetrics := range podsMetrics {
		reader := bytes.NewReader(podMetrics.Metrics)

		var metricsParser expfmt.TextParser

		parsedMetrics, err := metricsParser.TextToMetricFamilies(reader)
		if err != nil {
			return err
		}
		for _, family := range parsedMetrics {
			if family.GetName() != metric {
				continue
			}
			for _, metric := range family.Metric {
				labels := make(map[string]string)
				for _, label := range metric.Label {
					labels[label.GetName()] = label.GetValue()
				}
				key := ""
				for _, label := range groupBy {
					key += labels[string(label)] + ","
				}
				key = key[:len(key)-1]
				if metric.Counter != nil {
					initalValues[key] -= metric.GetCounter().GetValue()
				} else {
					initalValues[key] -= metric.GetUntyped().GetValue()
				}
				// fmt.Printf("key: %s, value: %f\n", key, initalValues[key])
			}
		}
	}

	duration, err := time.ParseDuration(timeWindow)
	if err != nil {
		return err
	}
	time.Sleep(duration)

	podsMetrics = k8s.GetMetrics(p.api, pods, k8s.ProxyAdminPortName, 30*time.Second, false)
	for _, podMetrics := range podsMetrics {
		reader := bytes.NewReader(podMetrics.Metrics)

		var metricsParser expfmt.TextParser

		parsedMetrics, err := metricsParser.TextToMetricFamilies(reader)
		if err != nil {
			return err
		}
		for _, family := range parsedMetrics {
			if family.GetName() != metric {
				continue
			}
			for _, metric := range family.Metric {
				labels := make(map[string]string)
				for _, label := range metric.Label {
					labels[label.GetName()] = label.GetValue()
				}
				key := ""
				for _, label := range groupBy {
					key += labels[string(label)] + ","
				}
				key = key[:len(key)-1]
				if metric.Counter != nil {
					initalValues[key] += metric.GetCounter().GetValue()
				} else {
					initalValues[key] += metric.GetUntyped().GetValue()
				}
				// fmt.Printf("key: %s, value: %f\n", key, initalValues[key])
			}
		}
	}

	for key, value := range initalValues {
		labels := make(model.LabelSet)
		labelValues := strings.Split(key, ",")
		for i, label := range groupBy {
			labels[label] = model.LabelValue(labelValues[i])
		}
		labels[model.LabelName(resource.Type)] = model.LabelValue(resource.Name)
		results <- &model.Sample{
			Metric:    model.Metric(labels),
			Value:     model.SampleValue(value),
			Timestamp: model.Time(time.Now().UnixMilli()),
		}
	}
	return nil
}

func (p ProxyMetrics) QueryQuantile(
	ctx context.Context,
	quantile float64,
	metric string,
	timeWindow string,
	labels model.LabelSet,
	groupBy model.LabelNames,
	resource *pb.Resource,
	results chan<- *model.Sample,
) error {
	defer close(results)
	pods, err := k8s.GetPodsFor(ctx, p.api, resource.Namespace, fmt.Sprintf("%s/%s", resource.Type, resource.Name))
	if err != nil {
		return err
	}

	initialValues := make(map[string]model.HistogramBuckets)

	podsMetrics := k8s.GetMetrics(p.api, pods, k8s.ProxyAdminPortName, 30*time.Second, false)
	for _, podMetrics := range podsMetrics {
		reader := bytes.NewReader(podMetrics.Metrics)

		var metricsParser expfmt.TextParser

		parsedMetrics, err := metricsParser.TextToMetricFamilies(reader)
		if err != nil {
			return err
		}
		for _, family := range parsedMetrics {
			if family.GetName()+"_bucket" != metric {
				continue
			}
			for _, metric := range family.Metric {
				labels := make(map[string]string)
				for _, label := range metric.Label {
					labels[label.GetName()] = label.GetValue()
				}
				key := ""
				for _, label := range groupBy {
					key += labels[string(label)] + ","
				}
				key = key[:len(key)-1]

				if initialValues[key] == nil {
					initialValues[key] = histogramNegative(intoHistogram(metric.GetHistogram()))
				} else {
					initialValues[key] = histogramMinus(initialValues[key], intoHistogram(metric.GetHistogram()))
				}
				// fmt.Printf("key: %s, value: %v\n", key, initialValues[key])
			}
		}
	}

	duration, err := time.ParseDuration(timeWindow)
	if err != nil {
		return err
	}
	time.Sleep(duration)

	podsMetrics = k8s.GetMetrics(p.api, pods, k8s.ProxyAdminPortName, 30*time.Second, false)
	for _, podMetrics := range podsMetrics {
		reader := bytes.NewReader(podMetrics.Metrics)

		var metricsParser expfmt.TextParser

		parsedMetrics, err := metricsParser.TextToMetricFamilies(reader)
		if err != nil {
			return err
		}
		for _, family := range parsedMetrics {
			if family.GetName()+"_bucket" != metric {
				continue
			}
			for _, metric := range family.Metric {
				labels := make(map[string]string)
				for _, label := range metric.Label {
					labels[label.GetName()] = label.GetValue()
				}
				key := ""
				for _, label := range groupBy {
					key += labels[string(label)] + ","
				}
				key = key[:len(key)-1]

				initialValues[key] = histogramAdd(initialValues[key], intoHistogram(metric.GetHistogram()))
				// fmt.Printf("key: %s, value: %v\n", key, initialValues[key])
			}
		}
	}

	for key, value := range initialValues {
		labels := make(model.LabelSet)
		labelValues := strings.Split(key, ",")
		for i, label := range groupBy {
			labels[label] = model.LabelValue(labelValues[i])
		}
		labels[model.LabelName(resource.Type)] = model.LabelValue(resource.Name)
		results <- &model.Sample{
			Metric:    model.Metric(labels),
			Value:     model.SampleValue(percentileFromHistogram(value, float64(quantile))),
			Timestamp: model.Time(time.Now().UnixMilli()),
		}
	}
	return nil
}

func intoHistogram(histogram *promModel.Histogram) model.HistogramBuckets {
	buckets := make(model.HistogramBuckets, len(histogram.Bucket))
	for i, bucket := range histogram.Bucket {
		buckets[i] = &model.HistogramBucket{
			Lower: model.FloatString(bucket.GetUpperBound()),
			Upper: model.FloatString(bucket.GetUpperBound()),
			Count: model.FloatString(bucket.GetCumulativeCount()),
		}
	}
	return buckets
}

func histogramMinus(a model.HistogramBuckets, b model.HistogramBuckets) model.HistogramBuckets {
	buckets := make(model.HistogramBuckets, len(a))
	for i := range a {
		if a[i].Upper != b[i].Upper {
			panic("histogramMinus: mismatched bucket boundaries")
		}
		if a[i].Lower != b[i].Lower {
			panic("histogramMinus: mismatched bucket boundaries")
		}
		buckets[i] = &model.HistogramBucket{
			Lower: a[i].Lower,
			Upper: a[i].Upper,
			Count: a[i].Count - b[i].Count,
		}
	}
	return buckets
}

func histogramAdd(a model.HistogramBuckets, b model.HistogramBuckets) model.HistogramBuckets {
	buckets := make(model.HistogramBuckets, len(a))
	for i := range a {
		if a[i].Upper != b[i].Upper {
			panic("histogramAdd: mismatched bucket boundaries")
		}
		if a[i].Lower != b[i].Lower {
			panic("histogramAdd: mismatched bucket boundaries")
		}
		buckets[i] = &model.HistogramBucket{
			Lower: a[i].Lower,
			Upper: a[i].Upper,
			Count: a[i].Count + b[i].Count,
		}
	}
	return buckets
}

func histogramNegative(a model.HistogramBuckets) model.HistogramBuckets {
	buckets := make(model.HistogramBuckets, len(a))
	for i := range buckets {
		buckets[i] = &model.HistogramBucket{
			Lower: a[i].Lower,
			Upper: a[i].Upper,
			Count: a[i].Count * -1,
		}
	}
	return buckets
}

func percentileFromHistogram(histogram model.HistogramBuckets, quantile float64) float64 {
	total := histogram[len(histogram)-1].Count
	target := total * model.FloatString(quantile)
	for i, bucket := range histogram {
		if bucket.Count >= target {
			width := bucket.Upper - bucket.Lower
			prev := model.FloatString(0.0)
			if i > 0 {
				prev = histogram[i-1].Count
			}
			offset := target - prev
			size := bucket.Count - prev
			ratio := offset / size
			return float64(bucket.Lower + width*ratio)
		}
	}
	panic("failed to find quantile")
}
