package servicemirror

import (
	"fmt"
	"reflect"
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/util/workqueue"
)

type BufferingProbeEventSink struct {
	events []interface{}
}

func (s *BufferingProbeEventSink) send(event interface{}) {
	s.events = append(s.events, event)
}

type probeEventsTestCase struct {
	description                      string
	environment                      *testEnvironment
	expectedEventsSentToProbeManager []interface{}
}

func (tc *probeEventsTestCase) run(t *testing.T) {
	t.Run(tc.description, func(t *testing.T) {
		q := workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())
		probeEventsSink := &BufferingProbeEventSink{}
		_, err := tc.environment.runEnvironment(probeEventsSink, q)
		if err != nil {
			t.Fatal(err)
		}

		expectedNumEvents := len(tc.expectedEventsSentToProbeManager)
		actualNumEvents := len(probeEventsSink.events)

		if expectedNumEvents != actualNumEvents {
			t.Fatalf("Was expecting %d events but got %d", expectedNumEvents, actualNumEvents)
		}

		for i, ev := range tc.expectedEventsSentToProbeManager {
			evInQueue := probeEventsSink.events[i]
			if !reflect.DeepEqual(ev, evInQueue) {
				t.Fatalf("was expecting to see event %T but got %T", ev, evInQueue)
			}
		}
	})
}

func gatewaySpec(name, namespace, clustername, address, resVersion, identity string, incomingPort, probePort uint32, probePath string, probePeriod uint32) GatewaySpec {
	return GatewaySpec{
		gatewayName:      name,
		gatewayNamespace: namespace,
		clusterName:      clustername,
		addresses: []v1.EndpointAddress{
			{
				IP: address,
			},
		},
		incomingPort:    incomingPort,
		resourceVersion: resVersion,
		identity:        identity,
		ProbeConfig: &ProbeConfig{
			path:            probePath,
			port:            probePort,
			periodInSeconds: probePeriod,
		},
	}
}

func TestRemoteServiceCreatedProbeEvents(t *testing.T) {
	for _, tt := range []probeEventsTestCase{
		{
			description:                      "do not send event if gateway cannot be resolved",
			environment:                      serviceCreateWithMissingGateway,
			expectedEventsSentToProbeManager: []interface{}{},
		},
		{
			description:                      "do not send event if gateway has wrong spec",
			environment:                      createServiceWrongGatewaySpec,
			expectedEventsSentToProbeManager: []interface{}{},
		},
		{
			description: "create service and endpoints when gateway can be resolved",
			environment: createServiceOkeGatewaySpec,
			expectedEventsSentToProbeManager: []interface{}{
				&MirroredServicePaired{
					serviceName:      fmt.Sprintf("service-one-%s", clusterName),
					serviceNamespace: "ns1",
					GatewaySpec:      gatewaySpec("existing-gateway", "existing-namespace", clusterName, "192.0.2.127", "222", "gateway-identity", 888, defaultProbePort, defaultProbePath, defaultProbePeriod),
				},
			},
		},
	} {
		tc := tt // pin
		tc.run(t)
	}
}

func TestRemoteServiceDeletedProbeEvents(t *testing.T) {
	for _, tt := range []probeEventsTestCase{
		{
			description: "send a service unpaired event",
			environment: deleteMirroredService,
			expectedEventsSentToProbeManager: []interface{}{
				&MirroredServiceUnpaired{
					serviceName:      fmt.Sprintf("test-service-remote-to-delete-%s", clusterName),
					serviceNamespace: "test-namespace-to-delete",
					gatewayName:      "gateway",
					gatewayNs:        "gateway-ns",
					clusterName:      clusterName,
				},
			},
		},
	} {
		tc := tt // pin
		tc.run(t)
	}
}

func TestRemoteServiceUpdatedProbeEvents(t *testing.T) {
	for _, tt := range []probeEventsTestCase{
		{
			description: "unpairs from old and pairs to new gateway",
			environment: updateServiceToNewGateway,
			expectedEventsSentToProbeManager: []interface{}{
				&MirroredServiceUnpaired{
					serviceName:      fmt.Sprintf("test-service-%s", clusterName),
					serviceNamespace: "test-namespace",
					gatewayName:      "gateway",
					gatewayNs:        "gateway-ns",
					clusterName:      clusterName,
				},
				&MirroredServicePaired{
					serviceName:      fmt.Sprintf("test-service-%s", clusterName),
					serviceNamespace: "test-namespace",
					GatewaySpec:      gatewaySpec("gateway-new", "gateway-ns", clusterName, "0.0.0.0", "currentGatewayResVersion", "", 999, defaultProbePort, defaultProbePath, defaultProbePeriod),
				},
			},
		},
		{
			description: "does not send event when gateway assignment does not change",
			environment: updateServiceWithChangedPorts,
		},
	} {
		tc := tt // pin
		tc.run(t)
	}
}
