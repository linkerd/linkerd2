package conduit

const (
	ApiRoot             = "/" // Must be absolute (with a leading slash).
	ApiVersion          = "v1"
	JsonContentType     = "application/json"
	ApiPrefix           = "api/" + ApiVersion + "/" // Must be relative (without a leading slash).
	ProtobufContentType = "application/octet-stream"
	ErrorHeader         = "conduit-error"
)
