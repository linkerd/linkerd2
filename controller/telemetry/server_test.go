package telemetry

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/prometheus/common/model"
	"github.com/runconduit/conduit/controller/api/public"
	read "github.com/runconduit/conduit/controller/gen/controller/telemetry"
)

type testResponse struct {
	err      error
	promRes  model.Value
	queryReq *read.QueryRequest
	queryRes *read.QueryResponse
}

func TestServerResponses(t *testing.T) {
	responses := []testResponse{

		testResponse{
			err:     errors.New("EndMs timestamp missing from request: query:\"fake query0\" "),
			promRes: &model.Scalar{},
			queryReq: &read.QueryRequest{
				Query: "fake query0",
			},
			queryRes: nil,
		},
		testResponse{
			err:     errors.New("Unexpected query result type (expected Vector): scalar"),
			promRes: &model.Scalar{},
			queryReq: &read.QueryRequest{
				Query: "fake query1",
				EndMs: 1,
			},
			queryRes: nil,
		},
		testResponse{
			err:     errors.New("Unexpected query result type (expected Vector): matrix"),
			promRes: model.Matrix{},
			queryReq: &read.QueryRequest{
				Query: "fake query2",
				EndMs: 1,
			},
			queryRes: nil,
		},
		testResponse{
			err:     errors.New("Unexpected query result type (expected Matrix): vector"),
			promRes: model.Vector{},
			queryReq: &read.QueryRequest{
				Query:   "fake query3",
				StartMs: 1,
				EndMs:   2,
				Step:    "10s",
			},
			queryRes: nil,
		},
		testResponse{
			err: nil,
			promRes: model.Vector{
				&model.Sample{
					Metric:    model.Metric{"fake label": "fake value"},
					Value:     123,
					Timestamp: 456,
				},
				&model.Sample{
					Metric:    model.Metric{"fake label2": "fake value2"},
					Value:     321,
					Timestamp: 654,
				},
			},
			queryReq: &read.QueryRequest{
				Query: "fake query4",
				EndMs: 1,
			},
			queryRes: &read.QueryResponse{
				Metrics: []*read.Sample{
					&read.Sample{
						Values: []*read.SampleValue{{Value: 123, TimestampMs: 456}},
						Labels: map[string]string{"fake label": "fake value"},
					},
					&read.Sample{
						Values: []*read.SampleValue{{Value: 321, TimestampMs: 654}},
						Labels: map[string]string{"fake label2": "fake value2"},
					},
				},
			},
		},
		testResponse{
			err: nil,
			promRes: model.Matrix{
				&model.SampleStream{
					Metric: model.Metric{"fake label": "fake value"},
					Values: []model.SamplePair{
						{Timestamp: 1, Value: 2},
						{Timestamp: 3, Value: 4},
					},
				},
				&model.SampleStream{
					Metric: model.Metric{"fake label2": "fake value2"},
					Values: []model.SamplePair{
						{Timestamp: 5, Value: 6},
						{Timestamp: 7, Value: 8},
					},
				},
			},
			queryReq: &read.QueryRequest{
				Query:   "fake query5",
				StartMs: 1,
				EndMs:   2,
				Step:    "10s",
			},
			queryRes: &read.QueryResponse{
				Metrics: []*read.Sample{
					&read.Sample{
						Values: []*read.SampleValue{
							{Value: 2, TimestampMs: 1},
							{Value: 4, TimestampMs: 3},
						},
						Labels: map[string]string{"fake label": "fake value"},
					},
					&read.Sample{
						Values: []*read.SampleValue{
							{Value: 6, TimestampMs: 5},
							{Value: 8, TimestampMs: 7},
						},
						Labels: map[string]string{"fake label2": "fake value2"},
					},
				},
			},
		},
	}

	t.Run("Queries return the expected responses", func(t *testing.T) {
		for _, tr := range responses {
			s := server{
				prometheusAPI: &public.MockProm{Res: tr.promRes},
			}
			res, err := s.Query(context.Background(), tr.queryReq)
			if err != nil || tr.err != nil {
				if (err == nil && tr.err != nil) ||
					(err != nil && tr.err == nil) ||
					(err.Error() != tr.err.Error()) {
					t.Fatalf("Unexpected error (Expected: %s, Got: %s)", tr.err, err)
				}
			}

			if !reflect.DeepEqual(res, tr.queryRes) {
				t.Fatalf("Unexpected response:\n%+v\n!=\n%+v", res, tr.queryRes)
			}
		}
	})
}
