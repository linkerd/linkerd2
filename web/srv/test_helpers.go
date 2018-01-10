package srv

func FakeServer() Server {
	return Server{
		templateDir: "../templates",
		reload:      true,
	}
}
