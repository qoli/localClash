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

Use the [feature table](istoreos-test-features.md) to select affected functions
and retain tested-version evidence. The [test SOP](istoreos-release-test-sop.md)
describes the shared execution steps and release-risk summary. QEMU interaction
tests cover the selected functions; Docker acceptance is retired, and QEMU
results do not imply physical-router acceptance.
