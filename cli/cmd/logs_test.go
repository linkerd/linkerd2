package cmd

import (
	"fmt"
	"testing"

	"github.com/wercker/stern/stern"
)

func assertConfig(actual, expected *stern.Config) bool{
	return actual.Template != nil &&
		actual.ContainerState == expected.ContainerState
}
func TestNewSternConfig(t *testing.T) {
	var (
		tests = []struct {
			testMessage string
			flags          *logFlags
			expectedConfig *stern.Config
			expectedErr    error
		}{
			{
				"default flags",
				&logFlags{
					containerState: "running",
					tail: -1,
				},
				&stern.Config{
				},
				nil,
			},
			{
				"valid pod regex",
				&logFlags{
					containerState: "running",
					tail: -1,
				},
				&stern.Config{
				},
				nil,
			},
		}
	)
	for _, tt := range tests {
		t.Run(tt.testMessage, func(t *testing.T) {
			c, err := tt.flags.NewSternConfig()
			if assertConfig(c, tt.expectedConfig) {
				fmt.Printf("Error: %v", err)
				t.Fatalf("Unexpected config:\ngot: %+v\nexpected: %+v", c, tt.expectedConfig)
			}

			if err != tt.expectedErr {
				t.Fatalf("Unexpected err:\ngot: %+v\nexpected: %+v", err, tt.expectedErr)
			}
		})

	}
}
