.DEFAULT_GOAL := build

GOPATH := $(shell go env | grep GOPATH | sed 's/GOPATH="\(.*\)"/\1/')
PATH := $(GOPATH)/bin:$(PATH)
export $(PATH)

BINARY=ponger
LD_FLAGS += -s -w
VERSION=$(shell git describe --tags --abbrev=0 2>/dev/null | sed -r "s:^v::g")
RSRC=README_TPL.md
ROUT=README.md

help:
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}'

readme-gen:
	cp -av "${RSRC}" "${ROUT}"
	sed -ri -e "s:\[\[tag\]\]:${VERSION}:g" -e "s:\[\[os\]\]:linux:g" -e "s:\[\[arch\]\]:amd64:g" "${ROUT}"

publish: clean fetch readme-gen ## Generate a release, and publish to GitHub.
	$(GOPATH)/bin/goreleaser

update-deps: fetch ## Adds any missing dependencies, removes unused deps, etc.
	$(GOPATH)/bin/govendor add -v +external
	$(GOPATH)/bin/govendor remove -v +unused
	$(GOPATH)/bin/govendor update -v +vendor

upgrade-deps: update-deps ## Upgrades all dependencies to the latest available versions and saves them.
	$(GOPATH)/bin/govendor fetch -v +vendor

fetch: ## Fetches the necessary dependencies to build.
	test -f $(GOPATH)/bin/govendor || go get -v github.com/kardianos/govendor
	$(GOPATH)/bin/govendor sync

clean: ## Cleans up generated files/folders from the build.
	/bin/rm -fv "${BINARY}" dist/

debug: clean fetch ## Runs the webserver with debug mode.
	go run -ldflags "${LD_FLAGS}" *.go -c config.toml -d

build: clean fetch ## Compile and generate a binary.
	go build -ldflags "${LD_FLAGS}" -x -v -o ${BINARY}
