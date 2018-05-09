export BABEL_ENV ?= development

.PHONY: build
build:
	@# Build the assets.
	yarn && yarn webpack

.PHONY: dev
dev:
	@# Run a dev server, to do development with.
	yarn webpack-dev-server &
	cd .. && ../bin/go-run . --webpack-dev-server=http://localhost:8080

.PHONY: run
run: build
	@# Run a local server (sans. reloading).
	cd .. && ../bin/go-run .

.PHONY: test
test:
	@# Run the tests
	yarn karma start --single-run

.PHONY: help
help: SHELL := /bin/bash
help:
	@# Output all targets available.
	@ echo "usage: make [target] ..."
	@ echo ""
	@eval "echo \"$$(grep -h -B1 $$'^\t@#' $(MAKEFILE_LIST) \
		| sed 's/@#//' \
		| awk \
			-v NO_COLOR="$(NO_COLOR)" \
			-v OK_COLOR="$(OK_COLOR)" \
			-v RS="--\n" \
			-v FS="\n" \
			-v OFS="@@" \
			'{ split($$1,target,":"); $$1=""; print OK_COLOR target[1] NO_COLOR $$0 }' \
		| sort \
		| awk \
			-v FS="@@" \
			-v OFS="\n" \
			'{ CMD=$$1; $$1=""; print CMD $$0 }')\""

.DEFAULT_GOAL := help
