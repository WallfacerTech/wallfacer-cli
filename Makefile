.PHONY: all build generate build-generator sync-spec

all: build-generator sync-spec generate build

sync-spec:
	cp -f ../sophon/storage/app/private/scribe/openapi.yaml openapi.yaml

VERSION ?= dev

build:
	go build -ldflags "-X main.version=$(VERSION)" -o wallfacer .

generate: build-generator
	./openapi-cli-generator generate openapi.yaml

# Build the code generator from the sibling openapi-cli-generator repo.
# Requires: git clone https://github.com/WallfacerTech/openapi-cli-generator.git ../openapi-cli-generator
build-generator:
	cd ../openapi-cli-generator && go build -o $(CURDIR)/openapi-cli-generator .
