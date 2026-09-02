# OpenWrt SDK 包 Makefile（方式 B，进阶）
#
# 用法：把本目录（luci-app-mywanip/）复制进 OpenWrt SDK 的 package/ 下，
# 需要 feeds 中有 luci（./scripts/feeds update luci && ./scripts/feeds install luci-base），
# 然后 make package/luci-app-mywanip/compile V=s

include $(TOPDIR)/rules.mk

PKG_NAME:=luci-app-mywanip
PKG_VERSION:=1.0.0
PKG_RELEASE:=1

LUCI_TITLE:=LuCI page for mywanipd (WAN IP query)
LUCI_PKGARCH:=all
LUCI_DEPENDS:=+luci-base +mywanipd

include $(TOPDIR)/feeds/luci/luci.mk

define Package/$(PKG_NAME)/install
	$(CP) ./files/* $(1)/
endef

$(eval $(call BuildPackage,$(PKG_NAME)))
