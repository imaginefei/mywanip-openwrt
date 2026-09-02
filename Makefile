.PHONY: all fmt vet test build ipk clean

all: build

fmt:
	go fmt ./...

vet:
	go vet ./...

test:
	go test ./...

build:
	scripts/build.sh

ipk:
	go run ./cmd/ipkbuild

clean:
	rm -rf dist release build
