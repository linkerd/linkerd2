// +build ignore

package main

import (
	"log"

	"github.com/linkerd/linkerd2/cli/static"
	"github.com/shurcooL/vfsgen"
)

func main() {
	err := vfsgen.Generate(static.Templates, vfsgen.Options{
		Filename:     "static/generated_templates.gogen.go",
		PackageName:  "static",
		BuildTags:    "prod",
		VariableName: "Templates",
	})
	if err != nil {
		log.Fatalln(err)
	}
}
