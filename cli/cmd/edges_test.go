package cmd

import (
	"testing"

	"github.com/linkerd/linkerd2/controller/api/public"
)

type edgesParamsExp struct {
	options         *edgesOptions
	resSrc          []string
	resSrcNamespace []string
	resDst          []string
	resDstNamespace []string
	resClient       []string
	resServer       []string
	resMsg          []string
	resourceType    string
	file            string
}

func TestEdges(t *testing.T) {
	// response content for SRC, DST, SRC_NS, DST_NS, CLIENT_ID, SERVER_ID and MSG
	var (
		resSrc = []string{
			"web",
			"vote-bot",
			"web",
			"linkerd-controller",
		}
		resDst = []string{
			"voting",
			"web",
			"emoji",
			"linkerd-prometheus",
		}
		resSrcNamespace = []string{
			"emojivoto",
			"emojivoto",
			"emojivoto",
			"linkerd",
		}
		resDstNamespace = []string{
			"emojivoto",
			"emojivoto",
			"emojivoto",
			"linkerd",
		}
		resClient = []string{
			"web.emojivoto.serviceaccount.identity.linkerd.cluster.local",
			"default.emojivoto.serviceaccount.identity.linkerd.cluster.local",
			"web.emojivoto.serviceaccount.identity.linkerd.cluster.local",
			"linkerd-controller.linkerd.identity.linkerd.cluster.local",
		}
		resServer = []string{
			"voting.emojivoto.serviceaccount.identity.linkerd.cluster.local",
			"web.emojivoto.serviceaccount.identity.linkerd.cluster.local",
			"emoji.emojivoto.serviceaccount.identity.linkerd.cluster.local",
			"linkerd-prometheus.linkerd.identity.linkerd.cluster.local",
		}
		resMsg = []string{"", "", "", ""}
	)

	options := newEdgesOptions()
	options.outputFormat = tableOutput
	options.allNamespaces = true
	t.Run("Returns edges", func(t *testing.T) {
		testEdgesCall(edgesParamsExp{
			options:         options,
			resourceType:    "deployment",
			resSrc:          resSrc,
			resSrcNamespace: resSrcNamespace,
			resDst:          resDst,
			resDstNamespace: resDstNamespace,
			resClient:       resClient,
			resServer:       resServer,
			resMsg:          resMsg,
			file:            "edges_one_output.golden",
		}, t)
	})

	options.outputFormat = jsonOutput
	t.Run("Returns edges (json)", func(t *testing.T) {
		testEdgesCall(edgesParamsExp{
			options:         options,
			resourceType:    "deployment",
			resSrc:          resSrc,
			resSrcNamespace: resSrcNamespace,
			resDst:          resDst,
			resDstNamespace: resDstNamespace,
			resClient:       resClient,
			resServer:       resServer,
			resMsg:          resMsg,
			file:            "edges_one_output_json.golden",
		}, t)
	})

	t.Run("Returns edges (wide)", func(t *testing.T) {
		options.outputFormat = wideOutput
		testEdgesCall(edgesParamsExp{
			options:         options,
			resourceType:    "deployment",
			resSrc:          resSrc,
			resSrcNamespace: resSrcNamespace,
			resDst:          resDst,
			resDstNamespace: resDstNamespace,
			resClient:       resClient,
			resServer:       resServer,
			resMsg:          resMsg,
			file:            "edges_wide_output.golden",
		}, t)
	})

	t.Run("Returns an error if outputFormat specified is not wide, table or json", func(t *testing.T) {
		options.outputFormat = "test"
		args := []string{"deployment"}
		expectedError := "--output supports table, json and wide"

		_, err := buildEdgesRequests(args, options)
		if err == nil || err.Error() != expectedError {
			t.Fatalf("Expected error [%s] instead got [%s]", expectedError, err)
		}
	})

	t.Run("Returns an error if request includes the resource name", func(t *testing.T) {
		options.outputFormat = tableOutput
		args := []string{"pod/pod-name"}
		expectedError := "Edges cannot be returned for a specific resource name; remove pod-name from query"

		_, err := buildEdgesRequests(args, options)
		if err == nil || err.Error() != expectedError {
			t.Fatalf("Expected error [%s] instead got [%s]", expectedError, err)
		}
	})

	t.Run("Returns an error if request is for authority", func(t *testing.T) {
		options.outputFormat = tableOutput
		args := []string{"authority"}
		expectedError := "Resource type is not supported: authority"

		_, err := buildEdgesRequests(args, options)
		if err == nil || err.Error() != expectedError {
			t.Fatalf("Expected error [%s] instead got [%s]", expectedError, err)
		}
	})

	t.Run("Returns an error if request is for service", func(t *testing.T) {
		options.outputFormat = tableOutput
		args := []string{"service"}
		expectedError := "Resource type is not supported: service"

		_, err := buildEdgesRequests(args, options)
		if err == nil || err.Error() != expectedError {
			t.Fatalf("Expected error [%s] instead got [%s]", expectedError, err)
		}
	})

	t.Run("Returns an error if request is for all resource types", func(t *testing.T) {
		options.outputFormat = tableOutput
		args := []string{"all"}
		expectedError := "Resource type is not supported: all"

		_, err := buildEdgesRequests(args, options)
		if err == nil || err.Error() != expectedError {
			t.Fatalf("Expected error [%s] instead got [%s]", expectedError, err)
		}
	})
}

func testEdgesCall(exp edgesParamsExp, t *testing.T) {
	mockClient := &public.MockAPIClient{}
	response := public.GenEdgesResponse(exp.resourceType, "all")

	mockClient.EdgesResponseToReturn = &response

	args := []string{"deployment"}
	reqs, err := buildEdgesRequests(args, exp.options)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	resp, err := requestEdgesFromAPI(mockClient, reqs[0])
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	rows := edgesRespToRows(resp)
	output := renderEdgeStats(rows, exp.options)

	diffTestdata(t, exp.file, output)
}
