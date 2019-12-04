package gateway

// Patch represents a slice of patch operations.
type Patch []PatchOperation

// PatchOperation represents a single json patch operation.
type PatchOperation struct {
	Op    string      `json:"op"`
	Path  string      `json:"path"`
	Value interface{} `json:"value,omitempty"`
}
