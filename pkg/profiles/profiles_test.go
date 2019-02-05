package profiles

import (
	"errors"
	"fmt"
	"testing"

	"github.com/linkerd/linkerd2/controller/gen/apis/serviceprofile/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type spByteExp struct {
	err error
	sp  string
}

type spExp struct {
	err error
	sp  v1alpha1.ServiceProfile
}

func TestValidate(t *testing.T) {
	expectations := []spByteExp{
		{
			err: nil,
			sp: `apiVersion: linkerd.io/v1alpha1
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
			sp: `apiVersion: linkerd.io/v1alpha1
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
			err: errors.New("ServiceProfile \"bad.svc.cluster.local\" has invalid name (must be \"<service>.<namespace>.svc.cluster.local\")"),
			sp: `apiVersion: linkerd.io/v1alpha1
kind: ServiceProfile
metadata:
  name: bad.svc.cluster.local
  namespace: linkerd-ns
spec:
  routes:
  - name: name-1
    condition:
      method: GET
      pathRegex: /route-1`,
		},
		{
			err: errors.New("ServiceProfile \"name.ns.svc.cluster\" has invalid name (must be \"<service>.<namespace>.svc.cluster.local\")"),
			sp: `apiVersion: linkerd.io/v1alpha1
kind: ServiceProfile
metadata:
  name: name.ns.svc.cluster
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
			sp: `apiVersion: linkerd.io/v1alpha1
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
			sp: `apiVersion: linkerd.io/v1alpha1
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
			sp: `apiVersion: linkerd.io/v1alpha1
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
			sp: `apiVersion: linkerd.io/v1alpha1
kind: ServiceProfile
metadata:
  name: name.ns.svc.cluster.local
  namespace: linkerd-ns
spec:`,
		},
		{
			err: errors.New("ServiceProfile \"name.ns.svc.cluster.local\" has a route with no condition"),
			sp: `apiVersion: linkerd.io/v1alpha1
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
			sp: `apiVersion: linkerd.io/v1alpha1
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
			sp: `apiVersion: linkerd.io/v1alpha1
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
			sp: `apiVersion: linkerd.io/v1alpha1
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
			sp: `apiVersion: linkerd.io/v1alpha1
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
			sp: `apiVersion: linkerd.io/v1alpha1
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
			sp: `apiVersion: linkerd.io/v1alpha1
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
			sp: `apiVersion: linkerd.io/v1alpha1
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
			sp: `apiVersion: linkerd.io/v1alpha1
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
			sp: `apiVersion: linkerd.io/v1alpha1
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
			err: errors.New("failed to validate ServiceProfile: error unmarshaling JSON: while decoding JSON: json: cannot unmarshal bool into Go struct field Range.min of type uint32"),
			sp: `apiVersion: linkerd.io/v1alpha1
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
	}

	for id, exp := range expectations {
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

func TestValidateSP(t *testing.T) {
	expectations := []spExp{
		{
			err: nil,
			sp: v1alpha1.ServiceProfile{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "name.ns.svc.cluster.local",
					Namespace: "ns",
				},
				Spec: v1alpha1.ServiceProfileSpec{
					Routes: []*v1alpha1.RouteSpec{
						&v1alpha1.RouteSpec{
							Name: "route1",
							Condition: &v1alpha1.RequestMatch{
								Method: "GET",
							},
						},
					},
				},
			},
		},
		{
			err: nil,
			sp: v1alpha1.ServiceProfile{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "name.ns.svc.cluster.local",
					Namespace: "ns",
				},
				Spec: v1alpha1.ServiceProfileSpec{
					Routes: []*v1alpha1.RouteSpec{
						&v1alpha1.RouteSpec{
							Name: "route1",
							Condition: &v1alpha1.RequestMatch{
								All: []*v1alpha1.RequestMatch{
									&v1alpha1.RequestMatch{
										Method: "GET",
									},
									&v1alpha1.RequestMatch{
										Not: &v1alpha1.RequestMatch{
											PathRegex: "/private/.*",
										},
									},
								},
							},
							ResponseClasses: []*v1alpha1.ResponseClass{
								&v1alpha1.ResponseClass{
									Condition: &v1alpha1.ResponseMatch{
										Status: &v1alpha1.Range{
											Min: 500,
											Max: 599,
										},
									},
									IsFailure: true,
								},
							},
						},
					},
				},
			},
		},
		{
			err: errors.New("ServiceProfile \"name.ns.svc.cluster.local\" has a route with no condition"),
			sp: v1alpha1.ServiceProfile{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "name.ns.svc.cluster.local",
					Namespace: "ns",
				},
				Spec: v1alpha1.ServiceProfileSpec{
					Routes: []*v1alpha1.RouteSpec{
						&v1alpha1.RouteSpec{
							Name: "route1",
						},
					},
				},
			},
		},
	}

	for id, exp := range expectations {
		t.Run(fmt.Sprintf("%d", id), func(t *testing.T) {
			err := ValidateSP(exp.sp)
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

func TestValidateName(t *testing.T) {
	expectations := []struct {
		err       error
		name      string
		service   string
		namespace string
	}{
		{
			nil,
			"service.ns.svc.cluster.local",
			"service",
			"ns",
		},
		{
			errors.New("ServiceProfile \"bad.name\" has invalid name (must be \"<service>.<namespace>.svc.cluster.local\")"),
			"bad.name",
			"",
			"",
		},
		{
			errors.New("ServiceProfile \"bad.svc.cluster.local\" has invalid name (must be \"<service>.<namespace>.svc.cluster.local\")"),
			"bad.svc.cluster.local",
			"",
			"",
		},
		{
			errors.New("ServiceProfile \"service.ns.svc.cluster.foo\" has invalid name (must be \"<service>.<namespace>.svc.cluster.local\")"),
			"service.ns.svc.cluster.foo",
			"",
			"",
		},
	}

	for id, exp := range expectations {
		t.Run(fmt.Sprintf("%d", id), func(t *testing.T) {
			service, namespace, err := ValidateName(exp.name)
			if service != exp.service {
				t.Fatalf("Unexpected service (Expected: %s, Got: %s)", exp.service, service)
			}
			if namespace != exp.namespace {
				t.Fatalf("Unexpected namespace (Expected: %s, Got: %s)", exp.namespace, namespace)
			}
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
