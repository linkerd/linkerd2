package profiles

import (
	"errors"
	"fmt"
	"testing"
)

type spExp struct {
	err error
	sp  string
}

func TestValidate(t *testing.T) {
	expectations := []spExp{
		{
			err: nil,
			sp: `apiVersion: linkerd.io/v1alpha2
kind: ServiceProfile
metadata:
  name: name.ns.svc.cluster.local
  namespace: linkerd-ns
spec:
  routes:
  - name: name-1
    condition:
      method: GET
      pathRegex: /route-1`,
		},
		{
			err: nil,
			sp: `apiVersion: linkerd.io/v1alpha2
kind: ServiceProfile
metadata:
  name: name.ns.svc.cluster.local
  namespace: linkerd-ns
spec:
  retryBudget:
    minRetriesPerSecond: 5
    retryRatio: 0.2
    ttl: 10ms
  routes:
  - name: name-1
    condition:
      method: GET
      pathRegex: /route-1
      any:
      - all:
        - method: POST
        - pathRegex: '/authors/\d+'
      - all:
        - not:
            method: DELETE
        - pathRegex: /info.txt
    responseClasses:
    - condition:
        status:
          min: 500
          max: 599
        all:
        - status:
            min: 500
            max: 599
        - not:
            status:
              min: 503`,
		},
		{
			err: errors.New("ServiceProfile \"^.^\" has invalid name: a DNS-1123 subdomain must consist of lower case alphanumeric characters, '-' or '.', and must start and end with an alphanumeric character (e.g. 'example.com', regex used for validation is '[a-z0-9]([-a-z0-9]*[a-z0-9])?(\\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*')"),
			sp: `apiVersion: linkerd.io/v1alpha2
kind: ServiceProfile
metadata:
  name: ^.^
  namespace: linkerd-ns
spec:
  routes:
  - name: name-1
    condition:
      method: GET
      pathRegex: /route-1`,
		},
		{
			err: errors.New("failed to validate ServiceProfile: error unmarshaling JSON: while decoding JSON: json: unknown field \"foo\""),
			sp: `apiVersion: linkerd.io/v1alpha2
kind: ServiceProfile
metadata:
  name: name.ns.svc.cluster.local
  namespace: linkerd-ns
spec:
  foo: bar
  routes:
  - name: name-1
    condition:
      method: GET
      pathRegex: /route-1`,
		},
		{
			err: errors.New("failed to validate ServiceProfile: error unmarshaling JSON: while decoding JSON: json: unknown field \"foo\""),
			sp: `apiVersion: linkerd.io/v1alpha2
kind: ServiceProfile
metadata:
  name: name.ns.svc.cluster.local
  namespace: linkerd-ns
spec:
  routes:
  - name: name-1
    foo: bar
    condition:
      method: GET
      pathRegex: /route-1`,
		},
		{
			err: errors.New("failed to validate ServiceProfile: error unmarshaling JSON: while decoding JSON: json: unknown field \"foo\""),
			sp: `apiVersion: linkerd.io/v1alpha2
kind: ServiceProfile
metadata:
  name: name.ns.svc.cluster.local
  namespace: linkerd-ns
spec:
  routes:
  - name: name-1
    condition:
      foo: bar
      method: GET
      pathRegex: /route-1`,
		},
		{
			err: errors.New("ServiceProfile \"name.ns.svc.cluster.local\" has no routes"),
			sp: `apiVersion: linkerd.io/v1alpha2
kind: ServiceProfile
metadata:
  name: name.ns.svc.cluster.local
  namespace: linkerd-ns
spec:`,
		},
		{
			err: errors.New("ServiceProfile \"name.ns.svc.cluster.local\" has a route with no condition"),
			sp: `apiVersion: linkerd.io/v1alpha2
kind: ServiceProfile
metadata:
  name: name.ns.svc.cluster.local
  namespace: linkerd-ns
spec:
  routes:
  - name: name-1`,
		},
		{
			err: errors.New("ServiceProfile \"name.ns.svc.cluster.local\" has a route with no name"),
			sp: `apiVersion: linkerd.io/v1alpha2
kind: ServiceProfile
metadata:
  name: name.ns.svc.cluster.local
  namespace: linkerd-ns
spec:
  routes:
  - condition:
      method: GET
      pathRegex: /route-1`,
		},
		{
			err: errors.New("failed to validate ServiceProfile: error unmarshaling JSON: while decoding JSON: json: unknown field \"foo\""),
			sp: `apiVersion: linkerd.io/v1alpha2
kind: ServiceProfile
metadata:
  name: name.ns.svc.cluster.local
  namespace: linkerd-ns
spec:
  routes:
  - name: name-1
    condition:
      foo: bar
      method: GET
      pathRegex: /route-1
      not:
        method: GET`,
		},
		{
			err: errors.New("ServiceProfile \"name.ns.svc.cluster.local\" has a route with no condition"),
			sp: `apiVersion: linkerd.io/v1alpha2
kind: ServiceProfile
metadata:
  name: name.ns.svc.cluster.local
  namespace: linkerd-ns
spec:
  routes:
  - name: name-1
    condition:`,
		},
		{
			err: errors.New("ServiceProfile \"name.ns.svc.cluster.local\" has a route with an invalid condition: A request match must have a field set"),
			sp: `apiVersion: linkerd.io/v1alpha2
kind: ServiceProfile
metadata:
  name: name.ns.svc.cluster.local
  namespace: linkerd-ns
spec:
  routes:
  - name: name-1
    condition:
      method:`,
		},
		{
			err: errors.New("ServiceProfile \"name.ns.svc.cluster.local\" has a response class with no condition"),
			sp: `apiVersion: linkerd.io/v1alpha2
kind: ServiceProfile
metadata:
  name: name.ns.svc.cluster.local
  namespace: linkerd-ns
spec:
  routes:
  - name: name-1
    condition:
      method: GET
      pathRegex: /route-1
    responseClasses:
    - condition:`,
		},
		{
			err: errors.New("ServiceProfile \"name.ns.svc.cluster.local\" has a response class with an invalid condition: A response match must have a field set"),
			sp: `apiVersion: linkerd.io/v1alpha2
kind: ServiceProfile
metadata:
  name: name.ns.svc.cluster.local
  namespace: linkerd-ns
spec:
  routes:
  - name: name-1
    condition:
      method: GET
      pathRegex: /route-1
    responseClasses:
    - condition:
        status:
          min: 500
          max: 599
        all:
        - status:`,
		},
		{
			err: errors.New("ServiceProfile \"name.ns.svc.cluster.local\" has a response class with an invalid condition: Range maximum must be between 100 and 599, inclusive"),
			sp: `apiVersion: linkerd.io/v1alpha2
kind: ServiceProfile
metadata:
  name: name.ns.svc.cluster.local
  namespace: linkerd-ns
spec:
  routes:
  - name: name-1
    condition:
      method: GET
      pathRegex: /route-1
    responseClasses:
    - condition:
        status:
          min: 500
          max: 600`,
		},
		{
			err: errors.New("ServiceProfile \"name.ns.svc.cluster.local\" has a response class with an invalid condition: Range maximum cannot be smaller than minimum"),
			sp: `apiVersion: linkerd.io/v1alpha2
kind: ServiceProfile
metadata:
  name: name.ns.svc.cluster.local
  namespace: linkerd-ns
spec:
  routes:
  - name: name-1
    condition:
      method: GET
      pathRegex: /route-1
    responseClasses:
    - condition:
        status:
          min: 500
          max: 599
        all:
        - status:
            min: 500
            max: 599
        - not:
            status:
              min: 300
              max: 200`,
		},
		{
			err: errors.New("ServiceProfile \"name.ns.svc.cluster.local\" has a response class with an invalid condition: Range minimum must be between 100 and 599, inclusive"),
			sp: `apiVersion: linkerd.io/v1alpha2
kind: ServiceProfile
metadata:
  name: name.ns.svc.cluster.local
  namespace: linkerd-ns
spec:
  routes:
  - name: name-1
    condition:
      method: GET
      pathRegex: /route-1
    responseClasses:
    - condition:
        status:
          min: 500
          max: 599
        all:
        - status:
            min: 500
            max: 599
        - not:
            status:
              min: 1`,
		},
		{
			err: errors.New("failed to validate ServiceProfile: error unmarshaling JSON: while decoding JSON: json: cannot unmarshal bool into Go struct field Range.spec.routes.responseClasses.condition.all.not.status.min of type uint32"),
			sp: `apiVersion: linkerd.io/v1alpha2
kind: ServiceProfile
metadata:
  name: name.ns.svc.cluster.local
  namespace: linkerd-ns
spec:
  routes:
  - name: name-1
    condition:
      method: GET
      pathRegex: /route-1
    responseClasses:
    - condition:
        status:
          min: 500
          max: 599
        all:
        - status:
            min: 500
            max: 599
        - not:
            status:
              min: false`,
		},
		{
			err: errors.New("ServiceProfile \"name.ns.svc.cluster.local\" RetryBudget missing TTL field"),
			sp: `apiVersion: linkerd.io/v1alpha2
kind: ServiceProfile
metadata:
  name: name.ns.svc.cluster.local
  namespace: linkerd-ns
spec:
  retryBudget:
    minRetriesPerSecond: 5
    retryRatio: 0.2
  routes:
  - name: name-1
    condition:
      method: GET
      pathRegex: /route-1`,
		},
		{
			err: errors.New("ServiceProfile \"name.ns.svc.cluster.local\" RetryBudget: time: invalid duration \"foo\""),
			sp: `apiVersion: linkerd.io/v1alpha2
kind: ServiceProfile
metadata:
  name: name.ns.svc.cluster.local
  namespace: linkerd-ns
spec:
  retryBudget:
    minRetriesPerSecond: 5
    retryRatio: 0.2
    ttl: foo
  routes:
  - name: name-1
    condition:
      method: GET
      pathRegex: /route-1`,
		},
		{
			err: errors.New("ServiceProfile \"name.ns.svc.cluster.local\" RetryBudget RetryRatio must be non-negative: -0.200000"),
			sp: `apiVersion: linkerd.io/v1alpha2
kind: ServiceProfile
metadata:
  name: name.ns.svc.cluster.local
  namespace: linkerd-ns
spec:
  retryBudget:
    minRetriesPerSecond: 5
    retryRatio: -0.2
    ttl: 10s
  routes:
  - name: name-1
    condition:
      method: GET
      pathRegex: /route-1`,
		},
		{
			err: errors.New("failed to validate ServiceProfile: error unmarshaling JSON: while decoding JSON: json: cannot unmarshal number -5 into Go struct field RetryBudget.spec.retryBudget.minRetriesPerSecond of type uint32"),
			sp: `apiVersion: linkerd.io/v1alpha2
kind: ServiceProfile
metadata:
  name: name.ns.svc.cluster.local
  namespace: linkerd-ns
spec:
  retryBudget:
    minRetriesPerSecond: -5
    retryRatio: 0.2
    ttl: 10s
  routes:
  - name: name-1
    condition:
      method: GET
      pathRegex: /route-1`,
		},
	}

	for id, exp := range expectations {
		exp := exp // pin
		t.Run(fmt.Sprintf("%d", id), func(t *testing.T) {
			err := Validate([]byte(exp.sp))
			if err != nil || exp.err != nil {
				if (err == nil && exp.err != nil) ||
					(err != nil && exp.err == nil) ||
					(err.Error() != exp.err.Error()) {
					t.Fatalf("Unexpected error (Expected: %s, Got: %s)", exp.err, err)
				}
			}
		})
	}
}
