.PHONY: all build generate build-generator sync-spec

# Resolve a version string for `wallfacer --version`:
#   * tagged commit  -> "v1.2.3"
#   * untagged       -> "v1.2.3-4-gabcdef" or "abcdef-dirty"
#   * no git history -> "dev"
VERSION ?= $(shell git describe --tags --dirty --always 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

all: build-generator sync-spec generate build

sync-spec:
	cp -f ../sophon/storage/app/private/scribe/openapi.yaml openapi.yaml

build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o wallfacer .

generate: build-generator
	./openapi-cli-generator generate openapi.yaml

# Build the code generator from the sibling openapi-cli-generator repo.
# Requires: git clone https://github.com/WallfacerTech/openapi-cli-generator.git ../openapi-cli-generator
build-generator:
	cd ../openapi-cli-generator && go build -o $(CURDIR)/openapi-cli-generator .
