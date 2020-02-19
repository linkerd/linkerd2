package linkerd2

type (

	// Tracing consists of the add-on configuration of the distributed tracing components sub-chart.
	Tracing struct {
		Enabled   bool       `json:"enabled"`
		Collector *Collector `json:"collector"`
		Jaeger    *Jaeger    `json:"jaeger"`
	}

	// Collector consists of the config values required for Trace collector
	Collector struct {
		Name      string     `json:"name"`
		Image     string     `json:"image"`
		Resources *Resources `json:"resources"`
	}

	// Jaeger consists of the config values required for Jaeger
	Jaeger struct {
		Name      string     `json:"name"`
		Image     string     `json:"image"`
		Resources *Resources `json:"resources"`
	}
)
