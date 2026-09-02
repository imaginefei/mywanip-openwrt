# OpenWrt SDK 包 Makefile（方式 B，进阶）
#
# 用法：
#   1. 在本机跑 scripts/build.sh 交叉编译
#   2. 把目标架构的 dist/<goarch>/mywanipd 拷贝到本目录（与此 Makefile 同级）
#   3. 把本目录（mywanipd/）复制进 OpenWrt SDK 的 package/ 下
#   4. make package/mywanipd/compile V=s
#
# 说明：采用“预编译二进制”策略，SDK 内不需要 Go 工具链。
# 若将来要进上游 feeds，应改用 golang-package.mk 从源码构建。

include $(TOPDIR)/rules.mk

PKG_NAME:=mywanipd
PKG_VERSION:=1.0.0
PKG_RELEASE:=1

PKG_MAINTAINER:=mywanipd
PKG_LICENSE:=MIT

include $(INCLUDE_DIR)/package.mk

define Package/$(PKG_NAME)
  SECTION:=net
  CATEGORY:=Network
  TITLE:=WAN IP HTTP service
  DEPENDS:=+libc
endef

define Package/$(PKG_NAME)/description
  Exposes the WAN interface (default pppoe-wan) IPv4/IPv6 addresses via HTTP:
  GET /ipv4, /ipv6 (plain text) and / (JSON). Pure-Go static binary.
endef

define Build/Compile
	# 预编译二进制，SDK 内不编译
endef

define Package/$(PKG_NAME)/install
	$(INSTALL_DIR) $(1)/usr/bin
	$(INSTALL_BIN) ./$(PKG_NAME) $(1)/usr/bin/$(PKG_NAME)
	$(INSTALL_DIR) $(1)/etc/init.d
	$(INSTALL_BIN) ./files/$(PKG_NAME).init $(1)/etc/init.d/$(PKG_NAME)
	$(INSTALL_DIR) $(1)/etc/config
	$(INSTALL_CONF) ./files/$(PKG_NAME).config $(1)/etc/config/mywanip
endef

define Package/$(PKG_NAME)/conffiles
/etc/config/mywanip
endef

$(eval $(call BuildPackage,$(PKG_NAME)))
