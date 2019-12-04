package nginx

const (
	// L5DHeaderTestsValueHTTP is a sample header used for testing purposes.
	L5DHeaderTestsValueHTTP = "proxy_set_header l5d-dst-override $service_name.$namespace.svc.cluster.local:$service_port;"

	// L5DHeaderTestsValueGRPC is a sample header used for testing purposes.
	L5DHeaderTestsValueGRPC = "grpc_set_header l5d-dst-override $service_name.$namespace.svc.cluster.local:$service_port;"
)
