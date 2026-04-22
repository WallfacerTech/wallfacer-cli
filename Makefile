.PHONY: build generate generator

build:
	go build -o wallfacer .

generate: generator
	./openapi-cli-generator generate openapi.yaml
	go build -o wallfacer .

generator:
	cd .. && go build -o wallfacer-cli/openapi-cli-generator .
