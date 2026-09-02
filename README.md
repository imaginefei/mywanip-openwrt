# mywanipd

OpenWrt/iStoreOS 上的 WAN 口 IP 查询服务。一个纯 Go 编写的轻量守护进程，读取指定接口（默认 PPPoE 的 `pppoe-wan`）上的 IPv4/IPv6 地址，通过 HTTP 对外提供查询；配套 `luci-app-mywanip` 在 LuCI 的「服务」菜单下提供配置页和实时状态。

两个 ipk 包，职责分离（OpenWrt 惯例：程序与页面分开）：

| 包 | 内容 |
| --- | --- |
| `mywanipd_<ver>_<arch>.ipk` | 守护进程二进制、procd init 脚本、默认 UCI 配置 |
| `luci-app-mywanip_<ver>_all.ipk` | LuCI 页面（菜单/ACL/JS view），架构无关 |

## HTTP 接口

| 路径 | 成功 | 失败 |
| --- | --- | --- |
| `GET /ipv4` | `200 text/plain`，裸 IPv4 + 换行 | `503`（接口上无可用 IPv4） |
| `GET /ipv6` | `200 text/plain`，裸 IPv6（仅全球单播 GUA） | `503`（接口上无 GUA） |
| `GET /` | `200 application/json`，`{"ipv4":"...","ipv6":"..."}` | 恒 200，取不到的字段为空串 |
| `OPTIONS *` | `204` + CORS 预检头 | — |
| 其他方法 | `405`（`Allow: GET, OPTIONS`） | — |

已开启 CORS（`Access-Control-Allow-Origin: *`），LuCI 页面跨端口直连本服务读取状态。每次请求实时读取接口，PPPoE 重拨换 IP 后立即生效。

```sh
curl http://192.168.1.1:9377/ipv4
# 203.0.113.7
curl http://192.168.1.1:9377/
# {"ipv4":"203.0.113.7","ipv6":"2001:db8::abcd"}
```

## 安装与使用（路由器）

1. 确认架构：
   ```sh
   opkg print-architecture
   # x86_64 软路由/虚拟机 -> x86_64
   # NanoPi R4S/R5S 等   -> aarch64_generic
   # MT7621 老款路由器   -> mipsel_24kc
   ```
2. 上传并安装（与固件同版本、架构对应的两个包）：
   ```sh
   scp release/<version>/mywanipd_*_<arch>.ipk root@路由器:/tmp/
   scp release/<version>/luci-app-mywanip_*_all.ipk root@路由器:/tmp/
   ssh root@路由器
   opkg install /tmp/mywanipd_*_<arch>.ipk /tmp/luci-app-mywanip_*_all.ipk
   ```
3. 启用（二选一）：
   - LuCI → 服务 → My WAN IP → 勾选「启用服务」→ 保存并应用
   - 命令行：
     ```sh
     uci set mywanip.main.enabled='1'
     uci commit mywanip
     /etc/init.d/mywanipd enable
     /etc/init.d/mywanipd start
     ```
4. 验证：
   ```sh
   logread | grep mywanipd          # 服务日志（stdout 进 logd）
   curl 127.0.0.1:9377/
   ip -4 addr show pppoe-wan        # 交叉核对
   ip -6 addr show pppoe-wan
   ```

卸载：`opkg remove luci-app-mywanip`（页面消失，服务保留）；`opkg remove mywanipd`（自动 stop，`/etc/config/mywanip` 作为 conffile 保留）。

> 防火墙无需配置：LAN 区域访问路由器自身端口默认放行。请勿把端口暴露到 WAN。

## 配置文件（UCI：/etc/config/mywanip）

```ini
config mywanip 'main'
    option enabled '0'          # 1 启用服务；装包默认 0（不自动起服务）
    option interface 'pppoe-wan' # 读取地址的接口设备名
    option listen ':9377'       # HTTP 监听地址，host:port
```

`listen` 写法：

| 值 | 含义 |
| --- | --- |
| `:9377` 或 `[::]:9377` | 双栈监听（默认/推荐） |
| `0.0.0.0:9377` | 仅 IPv4 |
| `127.0.0.1:9377` | 仅本机 |

注意裸 `::9377` 是非法写法（IPv6 必须加方括号），程序会启动报错并提示。

## 从源码构建（macOS/Linux）

零第三方依赖，只需 Go 1.24+：

```sh
make build     # 交叉编译 5 个架构到 dist/
make ipk       # 生成 6 个 ipk 到 release/<version>/
make test      # 单元测试
```

目标架构：x86_64、aarch64_generic、mips_24kc、mipsel_24kc、arm_cortex-a7。

**为什么不需要 musl-cross 等 C 交叉工具链？** 本项目 `CGO_ENABLED=0`，纯 Go 二进制不链接 libc（Linux 下直接发系统调用、用 netlink 枚举网卡），交叉编译只需 `GOOS/GOARCH` 环境变量。musl-cross（`x86_64-linux-musl-gcc` 等）是 CGO 场景（链接 C 库如 libubus、sqlite）才需要的工具链，且必须在 Linux 环境运行。

**为什么 ipk 打包不用 Docker/SDK？** OpenWrt 的 ipk 是 **gzip 压缩的 tar 包**（经典 ipkg 格式，外层 tar 内含 `./debian-binary`、`./data.tar.gz`、`./control.tar.gz`，与官方仓库产物一致）。Go 标准库 `archive/tar`+`compress/gzip` 即可确定性生成（`cmd/ipkbuild`，零时间戳、root 属主、文件名排序），macOS 原生一条 `make ipk` 出包，产物连打两次字节一致。注意：不要用 Debian 的 ar 格式 ipk，OpenWrt 24.10 的 opkg 会报 "Malformed package file"。如需进 OpenWrt SDK / 上游 feeds，`deploy/openwrt/*/Makefile` 提供了方式 B（预编译二进制策略）。

## 本地开发调试（macOS）

```sh
go run ./cmd/mywanipd -config dev/mywanip.conf.example
# 配置里 interface=en0、listen=127.0.0.1:9377
curl http://127.0.0.1:9377/
```

## 设计说明与已知限制

- **CGNAT 放行**：PPPoE 若拿到 `100.64.0.0/10` 运营商级 NAT 地址（非真公网），照常返回——这就是接口上的真实地址。RFC1918 私网地址也放行（双 NAT 诊断、本机联调需要）。本服务返回的是**接口地址**，不是经过外部探测的"真公网 IP"。
- **IPv6 只返回全球单播 GUA（2000::/3）**：自动排除链路本地 fe80::/10、ULA fc00::/7、IPv4-mapped 地址。
- **RFC4941 临时地址**：Go 标准库无法区分 IPv6 临时地址与稳定地址；OpenWrt 上 pppoe-wan 通常只有一个 GUA，实际影响很小。
- **配置只在启动时读取**；LuCI「保存并应用」通过 procd reload trigger 自动重启服务生效。
- **LuCI 架构**：页面为 OpenWrt 22.03+ 的现代客户端 JS view（`menu.d`/`acl.d`/`view/*.js`），不含任何 Lua，iStoreOS 24.10 实测适用。
