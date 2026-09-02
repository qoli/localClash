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

The single functional release gate for both repositories is the
[iStoreOS QEMU release test SOP](istoreos-release-test-sop.md). It owns the
environment, clean/upgrade baselines, task chain, evidence, and release decision.
Docker OpenWrt acceptance is retired; physical-router testing is not required
by this gate and must not be inferred from QEMU results.
