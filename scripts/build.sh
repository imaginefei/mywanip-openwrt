#!/bin/sh
# 交叉编译 mywanipd 的 5 个目标架构二进制（纯 Go，CGO_ENABLED=0）。
# 无需任何 C 交叉工具链（musl-cross 是 CGO 场景才需要的），macOS 直接运行。
set -eu

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

VERSION="$(git describe --tags --always --dirty 2>/dev/null || echo dev)"
LDFLAGS="-s -w -X main.version=${VERSION}"

# build GOOS GOARCH GOMIPS GOARM
build() {
	goos="$1"
	goarch="$2"
	mips="$3"
	arm="$4"
	out="dist/${goarch}"
	mkdir -p "$out"
	echo ">> building ${goos}/${goarch} (version ${VERSION})"
	CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" GOMIPS="$mips" GOARM="$arm" \
		go build -trimpath -ldflags "$LDFLAGS" -o "$out/mywanipd" ./cmd/mywanipd
}

build linux amd64  ""  ""   # x86_64 软路由/虚拟机
build linux arm64  ""  ""   # aarch64（R4S/R5S、树莓派4 等）
build linux mips   softfloat ""  # mips_24kc（老款 MT7620 等，无硬浮点）
build linux mipsle softfloat "" # mipsel_24kc（MT7621 等）
build linux arm    ""  7    # arm_cortex-a7（armv7 设备）

echo "done. artifacts in dist/"
