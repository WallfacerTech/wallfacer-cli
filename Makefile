.PHONY: build generate generator

build:
	go build -o wallfacer .

generate: generator
	./openapi-cli-generator generate openapi.yaml
	go build -o wallfacer .

# Build the code generator from the sibling openapi-cli-generator repo.
# Requires: git clone https://github.com/WallfacerTech/openapi-cli-generator.git ../openapi-cli-generator
generator:
	go build -o openapi-cli-generator ../openapi-cli-generator
