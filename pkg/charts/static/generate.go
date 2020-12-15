// +build ignore

package main

import (
	"github.com/linkerd/linkerd2/pkg/charts/static"
	"github.com/shurcooL/vfsgen"
	log "github.com/sirupsen/logrus"
)

func main() {
	err := vfsgen.Generate(static.Templates, vfsgen.Options{
		Filename:     "generated_linkerd_templates.gogen.go",
		PackageName:  "static",
		BuildTags:    "prod",
		VariableName: "Templates",
	})
	if err != nil {
		log.Fatalln(err)
	}
}
