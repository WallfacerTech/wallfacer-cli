.PHONY: all build generate build-generator

all: build-generator generate build

build:
	go build -o wallfacer .

generate: build-generator
	./openapi-cli-generator generate openapi.yaml

# Build the code generator from the sibling openapi-cli-generator repo.
# Requires: git clone https://github.com/WallfacerTech/openapi-cli-generator.git ../openapi-cli-generator
build-generator:
	go build -o openapi-cli-generator ../openapi-cli-generator
