# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## 项目概述

mywanipd 是 OpenWrt/iStoreOS 上的 WAN 口 IP 查询服务：Go 守护进程读取指定网卡（默认 PPPoE 的 `pppoe-wan`）上的 IPv4/IPv6 地址并通过 HTTP 暴露（`/ipv4`、`/ipv6` 纯文本，`/` JSON 汇总），配套一个 LuCI 配置页。目标设备是 iStoreOS 24.10.8（基于 OpenWrt 24.10），开发机为 macOS（Apple Silicon）。

**面向用户的产出物（commit message、代码注释、文档）使用中文。** conventional commit 前缀（feat/fix/build/chore/docs）保留英文，描述和正文用中文。

## 维护者背景与协作方式

- 维护者**熟悉 Go，但 OpenWrt 开发是新手**：Go 层面的写法不用解释；涉及 OpenWrt 概念（procd、ubus、UCI、ipk/opkg、LuCI、netifd 等）时需要简要说明"是什么、为什么这样做"。
- 维护者习惯**详细、分步骤的计划**，并会对技术决策追问理由（例如"为什么不需要 Docker/musl-cross"）。做方案时把权衡和替代方案讲清楚，不要只给结论。
- 开发机：Apple Silicon Mac，用 goenv 管理 Go（若 `go version` 报 shim 路径错误，跑 `goenv rehash` 修复）。
- 目标设备（维护者实测）：**iStoreOS 24.10.8**（2026073111 构建，基于 OpenWrt 24.10），上网方式 PPPoE（接口设备名 `pppoe-wan`）。上机验证在该设备进行；方式 B 的 SDK 需用 24.10 版本。

## 常用命令

```sh
make test       # go test ./...
make vet        # go vet ./...
make fmt        # go fmt ./...
make build      # scripts/build.sh：交叉编译 5 架构到 dist/<goarch>/mywanipd
make ipk        # go run ./cmd/ipkbuild：产出 6 个 ipk 到 release/<version>/
make ipk V=2.0.0-beta1   # 手动指定版本（不打 tag 发预览包时用）
make clean      # 清 dist/ release/

# 本机调试（macOS 上用 en0，配置见 dev/mywanip.conf.example）
go run ./cmd/mywanipd -config dev/mywanip.conf.example

# 跑单个测试
go test ./internal/ipsource/ -run TestIsValidIPv6 -v
go test ./cmd/ipkbuild/ -v

# 带版本号构建（release 用）：代码全部提交后
git tag v1.0.0 && make clean && make build && make ipk   # 产物落在 release/1.0.0/（ipkbuild 去掉 v 前缀）
```

环境前提：Go 1.24+；若 `go version` 报 shim 错误，跑 `goenv rehash` 修复。

**注意：不要用 `| head` 截断 `make build` / `make ipk` 的输出**——管道提前关闭会 SIGPIPE 中断构建，产出不完整且不易察觉。要看尾部用 `| tail`。

## 架构要点（跨多文件才能理解的部分）

**两个独立二进制，都在 cmd/ 下：**
- `cmd/mywanipd/` — 路由器上的守护进程。
- `cmd/ipkbuild/` — 开发机上的打包工具，把 `dist/` 二进制 + `deploy/openwrt/*/files/` 文本组装成 ipk。它本身不部署到路由器。

**数据流：** Go 源码 → `scripts/build.sh` → `dist/<goarch>/mywanipd`（gitignore，**二进制不入仓库**）→ `cmd/ipkbuild` 合并 `deploy/openwrt/` 里的 init 脚本/UCI 配置/LuCI 文件 → `release/*.ipk`。

**internal/ 三层依赖关系：** `config`（纯 Go UCI 解析器，零依赖）← `ipsource`（`net.InterfaceByName` 取地址 + 过滤规则）← `httpserver`（路由/CORS/文本与 JSON 响应，取址函数通过 `IPFunc` 注入以便测试）。`cmd/mywanipd` 只做装配：flag 解析、配置加载、信号优雅退出。

**配置模型（UCI `mywanip.main.*`）：** `enabled`、`interface`（默认 pppoe-wan）、`port`（默认 9377）、`bind_ipv4`/`bind_ipv6`（默认都开）。监听方式由两个 bind 开关组合：双开 = 双栈单 socket（`:port`）；仅 v4 = `tcp4 0.0.0.0`；仅 v6 = `tcp6 [::]` 且置 `IPV6_V6ONLY`；都关 = 拒绝启动。**配置只在启动时读一次**，reload 语义 = 重启进程；而 IP 地址每次 HTTP 请求实时从内核读（netlink），刻意无缓存——PPPoE 重拨换 IP 立即生效。

**关键约束：`CGO_ENABLED=0` 纯静态。** 取地址走标准库 `net`（Linux 下底层是 netlink），**刻意不链接 libubus/不引入 CGO**——这是交叉编译在 macOS 上只需 `GOOS/GOARCH` 而不需要 musl-cross 的前提。新增依赖前先确认不需要 cgo。MIPS 必须带 `GOMIPS=softfloat`（路由器无硬浮点），ARM 带 `GOARM=7`。

**IP 过滤规则（ipsource，有意为之，改前先读包注释）：**
- IPv4：剔除 loopback/链路本地/未指定；**放行 RFC1918 和 CGNAT 100.64/10**（PPPoE 运营商级 NAT 地址就是接口真实地址；放行私网也让 macOS en0 联调可行）。
- IPv6：只保留全球单播 GUA（2000::/3），多候选取字典序最小保证输出确定。
- 已知限制：stdlib 无法区分 RFC4941 临时地址（README 有说明）。

**ipk 格式（最容易踩的坑，勿改回 ar）：** OpenWrt 24.10 的 opkg 分发/接受的是 **gzip+tar（经典 ipkg 格式）**，不是 Debian 的 ar 归档。结构为 `gzip(tar)`，外层 tar 含 `./debian-binary`（内容 `2.0\n`）、`./data.tar.gz`、`./control.tar.gz`，**所有 tar 成员名必须带 `./` 前缀**，且 **tar 必须包含全部父目录条目**（TypeDir，0755）——opkg 解压不会自动 mkdir，缺目录项会报 `wfopen: ... No such file or directory`。ar 格式会在路由器上报 `Malformed package file`。`cmd/ipkbuild` 用 stdlib `archive/tar`+`compress/gzip` 生成，确定性要求：uid/gid=0、Uname/Gname=root、ModTime 取当前 git commit 时间（同 commit 字节一致且文件日期不显示 1970）、文件名排序（连打两次产物字节一致，有测试锁定）。本机验证格式用 `tar tzf xxx.ipk`（macOS bsdtar 与 opkg 同为 libarchive）。

**OpenWrt 集成（deploy/openwrt/）：**
- `mywanipd/files/mywanipd.init` 是 procd 脚本：读 UCI `mywanip.main.enabled` 门控启动，`procd_add_reload_trigger mywanip` 让「保存并应用」能触发 reload（程序只在启动时读配置，reload=重启）。
- **服务启停控制走官方 `luci.setInitAction`**（ubus 方法，luci-app-sqm/ddns 同款），不要用 `file.exec` 直调 init 脚本——ACL 的 exec 路径授权写法容易踩错且已弃用方向。acl.d 授权 = UCI 读写 + `read.ubus.luci: [setInitAction]`。
- **「保存并应用」不会拉起未启动过的服务**（踩过的坑）：procd 的 reload 触发器只在服务经 procd 启动后才注册，stop 后即删除。所以视图重写了 `handleSaveApply`：标准保存+应用完成后按 enabled 状态显式 enable+restart 或 stop。改 LuCI 行为时保持这个语义。
- ipk control 脚本：`postinst` 安装即 `enable`（注册 rc.d 开机自启；enabled=0 门控保证不实际启动）；`prerm` 里 stop 输出重定向到 /dev/null（服务未运行时 procd 回 ubus Not found，属无害噪音，静音）。
- LuCI 页面是**现代客户端 JS view 架构**（OpenWrt 22.03+，`menu.d/*.json` 菜单 + `acl.d/*.json` 权限 + `www/luci-static/resources/view/mywanip/mywanip.js` 视图），**不含任何 Lua，不需要 luci-compat**。docs/notes/ 里旧学习文档的 Lua CBI 写法已过时，不要照抄。操作成功提示用 `ui.addTimeLimitedNotification`（自动淡出），失败用持久 `ui.addNotification`。
- LuCI 页面状态展示靠浏览器跨端口 fetch 守护进程（mywanipd 已开 `Access-Control-Allow-Origin: *`），不走 rpcd/ubus。**ACL/菜单变更后必须退出 LuCI 重新登录**才生效（会话权限缓存）。
- 两个包分离：`mywanipd`（分架构）与 `luci-app-mywanip`（Architecture: all，Depends mywanipd）。`deploy/openwrt/*/Makefile` 是可选的 OpenWrt SDK 方式（方式 B），日常打包用 ipkbuild（方式 A）。

**版本号与发布：** `scripts/build.sh` 和 ipkbuild 都用 `git describe --tags --always --dirty`（HEAD 在 tag 上 = tag 本身；tag 后有提交 = `vX.Y.Z-N-gHASH`；无任何 tag = 短哈希），经 `-ldflags -X main.version=` 注入。ipk 包版本 = describe 去 `v` 前缀 + `-<pkgRelease>`（`cmd/ipkbuild` 里 `pkgRelease` 常量，即 OpenWrt `PKG_RELEASE` 发布号惯例：只改打包/页面不改程序时 bump 它让 opkg 识别升级；程序变了就打新 tag，发布号归 1）。二进制 `-version` 带 v 前缀、ipk 包名不带，同源不同展示。
