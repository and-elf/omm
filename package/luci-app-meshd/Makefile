# LuCI application package for OpenWrt Mesh Manager.
#
# Built against the LuCI feed (luci.mk), which installs htdocs/ (the view) and
# root/ (the rpcd plugin, its ACL, and the menu entry) automatically. Submit
# this under applications/luci-app-meshd/ in the LuCI feed, or build it from a
# local feed alongside the meshd package.

include $(TOPDIR)/rules.mk

LUCI_TITLE:=LuCI support for OpenWrt Mesh Manager (meshd)
LUCI_DEPENDS:=+meshd +curl
LUCI_PKGARCH:=all

PKG_MAINTAINER:=Andreas Elfving
PKG_LICENSE:=MIT

include $(TOPDIR)/feeds/luci/luci.mk

# call BuildPackage - OpenWrt buildroot signature
