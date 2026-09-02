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

# 版本号：默认取 git describe（tag 优先，无 tag 退回短哈希）；
# 用 V= 可手动指定，如 make ipk V=2.0.0-beta1
ipk:
	go run ./cmd/ipkbuild $(if $(V),-version $(V))

clean:
	rm -rf dist release build
