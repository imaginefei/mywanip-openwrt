# mywanipd

OpenWrt/iStoreOS 上的 WAN 口 IP 查询服务：一个 Go 编写的轻量守护进程，读取指定接口（默认 PPPoE 的 `pppoe-wan`）上的公网 IPv4/IPv6 地址，通过 HTTP 提供查询；配套 `luci-app-mywanip` 在 LuCI 的 Services 菜单下提供配置页。

## HTTP 接口

| 路径 | 返回 |
| --- | --- |
| `GET /ipv4` | 纯文本 IPv4 地址；无地址时 `503` |
| `GET /ipv6` | 纯文本 IPv6 地址（仅全球单播 GUA）；无地址时 `503` |
| `GET /` | JSON 汇总 `{"ipv4":"...","ipv6":"..."}`，恒 `200`，缺失项为空串 |

服务默认监听 `:9377`（双栈），已开启 CORS（`*`），LuCI 页面可直接跨端口 fetch。

## 快速开始（macOS 开发调试）

```sh
make test                      # 单元测试
go run ./cmd/mywanipd -config dev/mywanip.conf.example
# 另开终端：
curl http://127.0.0.1:9377/
curl http://127.0.0.1:9377/ipv4
```

## 构建与打包

```sh
make build     # 交叉编译 5 个架构到 dist/（CGO_ENABLED=0，纯静态，无需 C 工具链）
make ipk       # 生成 ipk 安装包到 release/（纯 Go 打包，无需 Docker/SDK）
```

目标架构：x86_64、aarch64_generic、mips_24kc、mipsel_24kc、arm_cortex-a7。

## OpenWrt 安装

```sh
opkg install mywanipd_<version>_<arch>.ipk
opkg install luci-app-mywanip_<version>_all.ipk
```

安装后在 LuCI → 服务 → My WAN IP 中启用服务并保存应用；或命令行：

```sh
uci set mywanip.main.enabled='1'
uci commit mywanip
/etc/init.d/mywanipd enable
/etc/init.d/mywanipd start
```

## 设计说明

- **纯 Go、零第三方依赖、`CGO_ENABLED=0`**：交叉编译只需 `GOOS/GOARCH`，不链接 libc，因此不需要 musl-cross 等 C 交叉工具链（那是 CGO 场景才需要的）。
- **取地址方式**：标准库 `net.InterfaceByName`（Linux 下走 netlink），不依赖 ubus/ubox。
- **CGNAT**：PPPoE 若拿到 `100.64.0.0/10` 运营商级 NAT 地址，仍会照常返回（这就是接口上的真实地址）。
- **IPv6 临时地址（RFC4941）限制**：标准库无法区分临时地址与稳定地址；OpenWrt 上 pppoe-wan 通常只有一个 GUA，实际影响很小。
- **配置**：UCI 文件 `/etc/config/mywanip`，仅在启动时读取；LuCI「保存并应用」通过 procd reload trigger 自动重启服务生效。

完整开发/打包文档见后续章节（TODO：阶段 8 补全）。
