package cmd

import (
	"testing"

	"github.com/linkerd/linkerd2/controller/api/public"
)

type edgesParamsExp struct {
	options      *edgesOptions
	resSrc       []string
	resDst       []string
	resClient    []string
	resServer    []string
	resMsg       []string
	resourceType string
	file         string
}

func TestEdges(t *testing.T) {
	// response content for SRC, DST, CLIENT, SERVER and MSG
	var (
		resSrc = []string{
			"web-57b7f9db85-297dw",
			"web-57b7f9db85-297dw",
			"vote-bot-7466ffc7f7-5rc4l",
		}
		resDst = []string{
			"emoji-646ddcc5f9-zjgs9",
			"voting-689f845d98-rj6nz",
			"web-57b7f9db85-297dw",
		}
		resClient = []string{
			"web.emojivoto.serviceaccount.identity.linkerd.cluster.local",
			"web.emojivoto.serviceaccount.identity.linkerd.cluster.local",
			"default.emojivoto.serviceaccount.identity.linkerd.cluster.local",
		}
		resServer = []string{
			"emoji.emojivoto.serviceaccount.identity.linkerd.cluster.local",
			"voting.emojivoto.serviceaccount.identity.linkerd.cluster.local",
			"web.emojivoto.serviceaccount.identity.linkerd.cluster.local",
		}
		resMsg = []string{"", "", ""}
	)

	options := newEdgesOptions()
	options.namespace = "emojivoto"
	options.outputFormat = tableOutput
	t.Run("Returns edges", func(t *testing.T) {
		testEdgesCall(edgesParamsExp{
			options:      options,
			resourceType: "pod",
			resSrc:       resSrc,
			resDst:       resDst,
			resClient:    resClient,
			resServer:    resServer,
			resMsg:       resMsg,
			file:         "edges_one_output.golden",
		}, t)
	})

	options.outputFormat = jsonOutput
	t.Run("Returns edges (json)", func(t *testing.T) {
		testEdgesCall(edgesParamsExp{
			options:      options,
			resourceType: "pod",
			resSrc:       resSrc,
			resDst:       resDst,
			resClient:    resClient,
			resServer:    resServer,
			resMsg:       resMsg,
			file:         "edges_one_output_json.golden",
		}, t)
	})

	t.Run("Returns an error if outputFormat specified is not table or json", func(t *testing.T) {
		options.outputFormat = wideOutput
		args := []string{"pod"}
		expectedError := "--output currently only supports table and json"

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
	response := public.GenEdgesResponse(exp.resourceType, exp.resSrc, exp.resDst, exp.resClient, exp.resServer, exp.resMsg)

	mockClient.EdgesResponseToReturn = &response

	args := []string{"pod"}
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
