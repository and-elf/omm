// Package luci holds tests for the OpenWrt LuCI integration assets that ship
// under package/luci-app-meshd/. The integration itself is shell/JSON (an rpcd
// exec plugin, ACLs, a menu entry), but the rpcd plugin has a precise
// request/response contract, so it is exercised here by running the script
// against a stub meshd HTTP server.
package luci
