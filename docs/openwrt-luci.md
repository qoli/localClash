# OpenWrt LuCI Support

The LuCI package design has moved to the standalone `localclash-luci` repo:

```text
/Volumes/Data/Github/localclash-luci/docs/openwrt-luci.md
```

This repository keeps the localClash Core: CLI/MCP, Mihomo runtime lifecycle,
configuration rendering/testing, component downloads, and versioned runtime
facts. The LuCI repository owns the OpenWrt package and the complete router
takeover module, including fw4/nft/policy-routing/DNS-hijack state, ownership
markers, boot/hotplug reconciliation, and runtime/takeover transactions.

For the local Docker OpenWrt and UTM OpenWrt test targets used to validate
that split, see `docs/openwrt-test-environments.md`.
