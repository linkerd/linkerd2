package testutil

import (
	"fmt"
	"strings"
	"time"
)

// TapEvent represents a tap event
type TapEvent struct {
	Method     string
	Authority  string
	Path       string
	HTTPStatus string
	GrpcStatus string
	TLS        string
	LineCount  int
}

// Tap executes a tap command and converts the command's streaming output into tap
// events using each line's "id" field
func Tap(target string, h *TestHelper, arg ...string) ([]*TapEvent, error) {
	cmd := append([]string{"viz", "tap", target}, arg...)
	outputStream, err := h.LinkerdRunStream(cmd...)
	if err != nil {
		return nil, err
	}
	defer outputStream.Stop()

	outputLines, err := outputStream.ReadUntil(10, 1*time.Minute)
	if err != nil {
		return nil, err
	}

	tapEventByID := make(map[string]*TapEvent)
	for _, line := range outputLines {
		fields := toFieldMap(line)
		obj, ok := tapEventByID[fields["id"]]
		if !ok {
			obj = &TapEvent{}
			tapEventByID[fields["id"]] = obj
		}
		obj.LineCount++
		obj.TLS = fields["tls"]

		switch fields["type"] {
		case "req":
			obj.Method = fields[":method"]
			obj.Authority = fields[":authority"]
			obj.Path = fields[":path"]
		case "rsp":
			obj.HTTPStatus = fields[":status"]
		case "end":
			obj.GrpcStatus = fields["grpc-status"]
		}
	}

	output := make([]*TapEvent, 0)
	for _, obj := range tapEventByID {
		if obj.LineCount == 3 { // filter out incomplete events
			output = append(output, obj)
		}
	}

	return output, nil
}

func toFieldMap(line string) map[string]string {
	fields := strings.Fields(line)
	fieldMap := map[string]string{"type": fields[0]}
	for _, field := range fields[1:] {
		parts := strings.SplitN(field, "=", 2)
		fieldMap[parts[0]] = parts[1]
	}
	return fieldMap
}

// ValidateExpected compares the received tap event with the expected tap event
func ValidateExpected(events []*TapEvent, expectedEvent TapEvent) error {
	if len(events) == 0 {
		return fmt.Errorf("Expected tap events, got nothing")
	}
	for _, event := range events {
		if *event != expectedEvent {
			return fmt.Errorf("Unexpected tap event [%+v]; expected=[%+v]", *event, expectedEvent)
		}
	}
	return nil
}
