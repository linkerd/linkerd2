package main

import (
	"fmt"
	"log"
	"net"
	"net/http"
)

type (
	ipv4Handler struct{}
	ipv6Handler struct{}
)

func (ipv4Handler) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	fmt.Fprintf(w, "IPv4\n")
}

func (ipv6Handler) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	fmt.Fprintf(w, "IPv6\n")
}

func main() {
	log.Print("Server started")

	go func() {
		ln, err := net.Listen("tcp4", "0.0.0.0:8080")
		if err != nil {
			log.Fatal(err)
		}
		srv := &http.Server{Handler: ipv4Handler{}}
		log.Fatal(srv.Serve(ln))
	}()

	ln, err := net.Listen("tcp6", "[::]:8080")
	if err != nil {
		log.Fatal(err)
	}
	srv := &http.Server{Handler: ipv6Handler{}}
	log.Fatal(srv.Serve(ln))
}
