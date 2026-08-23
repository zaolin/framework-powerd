.PHONY: build test test-cover test-python vet lint clean install all

BINARY := framework-powerd
BUILD_DIR := cmd/framework-powerd

all: build

build:
	go build -o $(BINARY) ./$(BUILD_DIR)

test:
	go test -race ./...

test-cover:
	go test -race -coverprofile=coverage.out ./...
	@go tool cover -func=coverage.out | tail -1

test-python:
	python -m pytest custom_components/framework_powerd/tests/ -q

vet:
	go vet ./...

lint: vet
	go build ./...

clean:
	rm -f $(BINARY) coverage.out

install: build
	sudo cp $(BINARY) /usr/local/bin/