package inject

import (
	l5dcharts "github.com/linkerd/linkerd2/pkg/charts/linkerd2"

	fuzz "github.com/AdaLogics/go-fuzz-headers"
)

// FuzzInject fuzzes Pod injection.
func FuzzInject(data []byte) int {
	f := fuzz.NewConsumer(data)
	yamlBytes, err := f.GetBytes()
	if err != nil {
		return 0
	}

	v := &l5dcharts.Values{}
	err = f.GenerateStruct(v)
	if err != nil {
		return 0
	}
	conf := NewResourceConfig(v, OriginUnknown, "")
	_, _ = conf.ParseMetaAndYAML(yamlBytes)
	injectProxy, err := f.GetBool()
	if err != nil {
		return 0
	}

	values, err := GetOverriddenValues(conf)
	if err != nil {
		return 0
	}

	_, _ = GetPodPatch(conf, injectProxy, values)
	_, _ = conf.CreateOpaquePortsPatch()

	report := &Report{}
	err = f.GenerateStruct(report)
	if err == nil {
		_, _ = conf.Uninject(report)
	}
	return 1
}
