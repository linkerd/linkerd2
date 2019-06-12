package api

const (
	apiVersion    = "v1"
	ApiPrefix     = "api/" + apiVersion + "/" // Must be relative (without a leading slash).
	ApiPort       = 8085
	ApiDeployment = "linkerd-controller"
)
