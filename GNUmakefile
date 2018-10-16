export GO111MODULE=off
REV?=$(shell git rev-parse --short HEAD)
VER?=$(shell git branch --format "%(refname:lstrip=2)")

help: ## print this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-16s\033[0m %s\n", $$1, $$2}'

lint: ## run gometalinter
	@gometalinter ./...

test: ## run tests
	@go test -v -race ./...

coverage: ## run coverage tests for coveralls
	@go get github.com/mattn/goveralls github.com/modocache/gover
	@go list -f '{{if len .TestGoFiles}}"go test -v -race -coverprofile={{.Dir}}/.coverprofile {{.ImportPath}}"{{end}}' ./... | xargs -L 1 sh -c
	@gover
	@goveralls -coverprofile=coverage.out -service=travis-ci -repotoken $(COVERALLS_TOKEN)

tools: ## install tools
	@go get -u github.com/mitchellh/gox
	@go get -u github.com/alecthomas/gometalinter
	@gometalinter --install

vendor-status: ## check vendor files
	GO111MODULE=on go mod verify

compile: ## compile
	gox -os="linux darwin windows" \
	  -arch="amd64" \
	  -output="dist/clon_{{.OS}}_{{.Arch}}" \
	  -ldflags '-X github.com/spirius/clon/cmd/clon/cmd.Revision=$(REV) -extldflags "-static"' \
	  -verbose ./cmd/clon

.PHONY: lint test tools vendor-status help
