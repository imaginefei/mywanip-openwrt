# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## 项目概述

mywanipd 是 OpenWrt/iStoreOS 上的 WAN 口 IP 查询服务：Go 守护进程读取指定网卡（默认 PPPoE 的 `pppoe-wan`）上的 IPv4/IPv6 地址并通过 HTTP 暴露（`/ipv4`、`/ipv6` 纯文本，`/` JSON 汇总），配套一个 LuCI 配置页。目标设备是 iStoreOS 24.10.8（基于 OpenWrt 24.10），开发机为 macOS（Apple Silicon）。

**面向用户的产出物（commit message、代码注释、文档）使用中文。** conventional commit 前缀（feat/fix/build/chore/docs）保留英文，描述和正文用中文。

## 常用命令

```sh
make test       # go test ./...
make vet        # go vet ./...
make fmt        # go fmt ./...
make build      # scripts/build.sh：交叉编译 5 架构到 dist/<goarch>/mywanipd
make ipk        # go run ./cmd/ipkbuild：产出 6 个 ipk 到 release/<version>/
make clean      # 清 dist/ release/

# 本机调试（macOS 上用 en0，配置见 dev/mywanip.conf.example）
go run ./cmd/mywanipd -config dev/mywanip.conf.example

# 跑单个测试
go test ./internal/ipsource/ -run TestIsValidIPv6 -v
go test ./cmd/ipkbuild/ -v

# 带版本号构建（release 用）：代码全部提交后
git tag v1.0.0 && make clean && make build && make ipk   # 产物落在 release/v1.0.0/
```

环境前提：Go 1.24+；若 `go version` 报 shim 错误，跑 `goenv rehash` 修复。

## 架构要点（跨多文件才能理解的部分）

**两个独立二进制，都在 cmd/ 下：**
- `cmd/mywanipd/` — 路由器上的守护进程。
- `cmd/ipkbuild/` — 开发机上的打包工具，把 `dist/` 二进制 + `deploy/openwrt/*/files/` 文本组装成 ipk。它本身不部署到路由器。

**数据流：** Go 源码 → `scripts/build.sh` → `dist/<goarch>/mywanipd`（gitignore，**二进制不入仓库**）→ `cmd/ipkbuild` 合并 `deploy/openwrt/` 里的 init 脚本/UCI 配置/LuCI 文件 → `release/*.ipk`。

**internal/ 三层依赖关系：** `config`（纯 Go UCI 解析器，零依赖）← `ipsource`（`net.InterfaceByName` 取地址 + 过滤规则）← `httpserver`（路由/CORS/文本与 JSON 响应，取址函数通过 `IPFunc` 注入以便测试）。`cmd/mywanipd` 只做装配：flag 解析、配置加载、信号优雅退出。

**关键约束：`CGO_ENABLED=0` 纯静态。** 取地址走标准库 `net`（Linux 下底层是 netlink），**刻意不链接 libubus/不引入 CGO**——这是交叉编译在 macOS 上只需 `GOOS/GOARCH` 而不需要 musl-cross 的前提。新增依赖前先确认不需要 cgo。MIPS 必须带 `GOMIPS=softfloat`（路由器无硬浮点），ARM 带 `GOARM=7`。

**IP 过滤规则（ipsource，有意为之，改前先读包注释）：**
- IPv4：剔除 loopback/链路本地/未指定；**放行 RFC1918 和 CGNAT 100.64/10**（PPPoE 运营商级 NAT 地址就是接口真实地址；放行私网也让 macOS en0 联调可行）。
- IPv6：只保留全球单播 GUA（2000::/3），多候选取字典序最小保证输出确定。
- 已知限制：stdlib 无法区分 RFC4941 临时地址（README 有说明）。

**ipk 格式（最容易踩的坑，勿改回 ar）：** OpenWrt 24.10 的 opkg 分发/接受的是 **gzip+tar（经典 ipkg 格式）**，不是 Debian 的 ar 归档。结构为 `gzip(tar)`，外层 tar 含 `./debian-binary`（内容 `2.0\n`）、`./data.tar.gz`、`./control.tar.gz`，**所有 tar 成员名必须带 `./` 前缀**。ar 格式会在路由器上报 `Malformed package file`。`cmd/ipkbuild` 用 stdlib `archive/tar`+`compress/gzip` 生成，确定性要求：uid/gid=0、Uname/Gname=root、ModTime 零值、文件名排序（连打两次产物字节一致，有测试锁定）。本机验证格式用 `tar tzf xxx.ipk`（macOS bsdtar 与 opkg 同为 libarchive）。

**OpenWrt 集成（deploy/openwrt/）：**
- `mywanipd/files/mywanipd.init` 是 procd 脚本：读 UCI `mywanip.main.enabled` 门控启动，`procd_add_reload_trigger mywanip` 让 LuCI「保存并应用」自动重启服务（程序只在启动时读配置，reload=重启）。
- LuCI 页面是**现代客户端 JS view 架构**（OpenWrt 22.03+，`menu.d/*.json` 菜单 + `acl.d/*.json` 权限 + `www/luci-static/resources/view/mywanip/mywanip.js` 视图），**不含任何 Lua，不需要 luci-compat**。docs/notes/ 里旧学习文档的 Lua CBI 写法已过时，不要照抄。
- LuCI 页面状态展示靠浏览器跨端口 fetch 守护进程（mywanipd 已开 `Access-Control-Allow-Origin: *`），不走 rpcd/ubus，所以 acl.d 只需 UCI 读写授权。
- 两个包分离：`mywanipd`（分架构）与 `luci-app-mywanip`（Architecture: all，Depends mywanipd）。`deploy/openwrt/*/Makefile` 是可选的 OpenWrt SDK 方式（方式 B），日常打包用 ipkbuild（方式 A）。

**版本号：** `scripts/build.sh` 和 ipkbuild 都用 `git describe --tags --always --dirty`（无 tag 时是短哈希，dirty 表示有未提交改动），经 `-ldflags -X main.version=` 注入；ipk 版本号同理，文件夹名即版本。
