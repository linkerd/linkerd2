package srv

// FakeServer provides a mock of a Server in `/web/srv`.
func FakeServer() Server {
	return Server{
		templateDir: "../templates",
		reload:      true,
	}
}
