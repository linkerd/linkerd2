package main

import (
	validator "github.com/linkerd/linkerd2/controller/sp-validator"
	"github.com/linkerd/linkerd2/controller/webhook"
)

func main() {
	webhook.Launch(
		nil,
		9997,
		validator.AdmitSP,
	)
}
