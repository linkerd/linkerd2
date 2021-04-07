package public

import (
	"bufio"
	"bytes"
	"fmt"
	"testing"

	"github.com/golang/protobuf/proto"
	"github.com/linkerd/linkerd2/pkg/protohttp"
	pb "github.com/linkerd/linkerd2/viz/metrics-api/gen/viz"
)

func TestFromByteStreamToProtocolBuffers(t *testing.T) {
	t.Run("Correctly marshalls a large byte array", func(t *testing.T) {
		rows := make([]*pb.StatTable_PodGroup_Row, 0)

		numberOfResourcesInMessage := 400
		for i := 0; i < numberOfResourcesInMessage; i++ {
			rows = append(rows, &pb.StatTable_PodGroup_Row{
				Resource: &pb.Resource{
					Namespace: "default",
					Name:      fmt.Sprintf("deployment%d", i),
					Type:      "deployment",
				},
			})
		}

		msg := pb.StatSummaryResponse{
			Response: &pb.StatSummaryResponse_Ok_{
				Ok: &pb.StatSummaryResponse_Ok{
					StatTables: []*pb.StatTable{
						{
							Table: &pb.StatTable_PodGroup_{
								PodGroup: &pb.StatTable_PodGroup{
									Rows: rows,
								},
							},
						},
					},
				},
			},
		}

		reader := bufferedReader(t, &msg)

		protobufMessageToBeFilledWithData := &pb.StatSummaryResponse{}
		err := protohttp.FromByteStreamToProtocolBuffers(reader, protobufMessageToBeFilledWithData)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
	})

	t.Run("When byte array contains error, treats stream as regular protobuf object", func(t *testing.T) {
		apiError := pb.ApiError{Error: "an error occurred"}

		var protobufMessageToBeFilledWithData pb.ApiError
		reader := bufferedReader(t, &apiError)
		err := protohttp.FromByteStreamToProtocolBuffers(reader, &protobufMessageToBeFilledWithData)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		expectedErrorMessage := apiError.Error
		actualErrorMessage := protobufMessageToBeFilledWithData.Error
		if actualErrorMessage != expectedErrorMessage {
			t.Fatalf("Expected object to contain message [%s], but got [%s]", expectedErrorMessage, actualErrorMessage)
		}
	})
}

func bufferedReader(t *testing.T, msg proto.Message) *bufio.Reader {
	msgBytes, err := proto.Marshal(msg)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	payload := protohttp.SerializeAsPayload(msgBytes)

	return bufio.NewReader(bytes.NewReader(payload))
}
