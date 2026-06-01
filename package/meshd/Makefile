# OpenWrt feed package for the meshd daemon.
#
# Builds the static, CGO-free Go binary with golang-package.mk and installs it
# alongside its procd init script and UCI defaults. Intended for submission
# under net/meshd/ in the official packages feed (or a local feed); OpenWrt's
# build infrastructure then compiles it for every architecture, so the per-arch
# matrix in the release workflow is only needed for the direct-download builds.
#
# Bump PKG_VERSION to the released tag and refresh PKG_HASH with the sha256 of
# the source tarball:
#   curl -sL https://codeload.github.com/and-elf/omm/tar.gz/v<version> | sha256sum

include $(TOPDIR)/rules.mk

PKG_NAME:=meshd
PKG_VERSION:=0.0.2
PKG_RELEASE:=1

PKG_SOURCE:=$(PKG_NAME)-$(PKG_VERSION).tar.gz
PKG_SOURCE_URL:=https://codeload.github.com/and-elf/omm/tar.gz/v$(PKG_VERSION)?
PKG_HASH:=b12e8746022728eafd6400bc5bcb5bffbf19afc3684a07ba585e29500a944d34
PKG_BUILD_DIR:=$(BUILD_DIR)/omm-$(PKG_VERSION)

PKG_MAINTAINER:=Andreas Elfving
PKG_LICENSE:=MIT
PKG_LICENSE_FILES:=LICENSE

PKG_BUILD_DEPENDS:=golang/host
PKG_BUILD_PARALLEL:=1

GO_PKG:=github.com/and-elf/omm
GO_PKG_BUILD_PKG:=github.com/and-elf/omm/meshd/cmd/meshd

include $(INCLUDE_DIR)/package.mk
include ../../lang/golang/golang-package.mk

define Package/meshd
  SECTION:=net
  CATEGORY:=Network
  TITLE:=OpenWrt Mesh Manager daemon
  URL:=https://github.com/and-elf/omm
  DEPENDS:=$(GO_ARCH_DEPENDS) +ubus
endef

define Package/meshd/description
  Local-first mesh networking management daemon for OpenWrt: node enrollment,
  automatic mesh configuration, topology collection and an embedded management
  PWA. Orchestrates existing OpenWrt services (netifd, hostapd, batman-adv) via
  ubus/UCI rather than replacing them.
endef

define Package/meshd/conffiles
/etc/config/meshd
endef

define Package/meshd/install
	$(call GoPackage/Package/Install/Bin,$(1))
	$(INSTALL_DIR) $(1)/etc/init.d $(1)/etc/config
	$(INSTALL_BIN) ./files/meshd.init $(1)/etc/init.d/meshd
	$(INSTALL_CONF) ./files/meshd.config $(1)/etc/config/meshd
endef

define Package/meshd/postinst
#!/bin/sh
[ -n "$${IPKG_INSTROOT}" ] && exit 0
/etc/init.d/meshd enable 2>/dev/null || true
exit 0
endef

$(eval $(call GoBinPackage,meshd))
$(eval $(call BuildPackage,meshd))
