.PHONY: all build generate build-generator sync-spec sync-spec-local

all: build-generator sync-spec generate build

# The API serves the spec of exactly the code deployed in production
# (generated into the Vapor artifact at deploy time; never committed to
# sophon). Override SPEC_URL to regenerate against another environment, or
# use sync-spec-local to copy from a sibling sophon checkout instead.
SPEC_URL ?= https://api.wallfacer.ai/v1/openapi.yaml

sync-spec:
	curl -fsSL "$(SPEC_URL)" -o openapi.yaml
	perl -0pi -e 's/^([ \t]*)properties: \[\][ \t]*$$/$${1}properties: {  }/mg' openapi.yaml

sync-spec-local:
	cp -f ../sophon/storage/app/private/scribe/openapi.yaml openapi.yaml
	perl -0pi -e 's/^([ \t]*)properties: \[\][ \t]*$$/$${1}properties: {  }/mg' openapi.yaml

VERSION ?= dev

build:
	go build -ldflags "-X main.version=$(VERSION)" -o wallfacer .

generate: build-generator
	./openapi-cli-generator generate openapi.yaml

# Build the code generator from the sibling openapi-cli-generator repo.
# Requires: git clone https://github.com/WallfacerTech/openapi-cli-generator.git ../openapi-cli-generator
build-generator:
	cd ../openapi-cli-generator && go build -o $(CURDIR)/openapi-cli-generator .
