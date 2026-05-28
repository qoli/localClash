目前 localClash MCP 工具可以理解成 6 組，核心目標是讓 Agent 能「先觀察、再規劃、再生成、最後執行」，避免一上來就動路由器網路。

**1. 自我發現與環境診斷**

- `tools_list`：給不會展示 MCP registry 的 Agent 自己查工具清單。
- `environment_inspect`：看主機、網路能力、localClash 狀態，不暴露憑證。
- `doctor`：做 read-only 診斷。
- `nl_file`：帶行號讀檔，讓 Agent 能引用證據或追 task log。
- `sed_file`：repo-local 文本修補，預設 dry-run。

服務場景：Agent 剛連上 MCP 時，先判斷「這是什麼、能不能動、風險多大」。

**2. 訂閱與節點素材**

- `subscriptions_status`：看訂閱來源與本地 effective subscription 狀態。
- `subscriptions_configure`：寫入訂閱來源，不立即刷新。
- `subscriptions_refresh`：下載訂閱、生成本地 artifact、合併成 `subscription.gob`。
- `subscription_nodes_list`：列出節點摘要。
- `subscription_nodes_search`：按名稱搜尋節點。

服務場景：小白給訂閱 URL 後，Agent 建立節點池，並能查 HK/JP/US 等節點候選。

**3. 規則包與規則證據**

- `packs_list`：查有哪些可用規則包。
- `packs_get`：用 `{source, pack}` 看單個規則包詳情。
- `pack_rules_prefetch`：下載 provider rules 到本地 cache。
- `pack_rules_read`：用 `{source, pack}` 讀某個 pack 的規則，必要時下載。
- `pack_rules_query`：在本地 cache 搜 domain/keyword，回答「某個域名應該落在哪類規則」。

服務場景：Agent 不憑空猜 Telegram、Steam、AI、Apple，而是查 rule pack 的實際規則證據。
這組工具的 pack selector 只接受 `{source, pack}`，例如
`{"source":"sukkaw","pack":"ai"}`；`packs_list` / `packs_get` 會返回可直接複製
的 `tool_args`。`sukkaw_ai`、`syncnext_SyncnextProxy` 這類 composite provider
或 renderer 名稱不是 MCP selector，只能出現在明確讀取 generated config/file 的
原始 Mihomo 內容裡。

**4. 配置狀態、構建與 Patch 工作流**

- `config_status`：看 `localclash.json`、`generated/mihomo.yaml`、render readiness、patch 狀態。預設輕量，`resolve=true/detail=true` 才做重查。
- `config_configure`：改核心、runtime profile、policy template。
- `proxy_group_build`：建立出口組，例如 HK/JP/US/⚡ 自动选择。
- `policy_group_build`：建立業務組，例如 Steam -> HK/JP/US。
- `custom_rules_build`：建立自訂 domain/CIDR 規則。
- `rule_provider_build`：建立外部 rule-provider intent。
- `config_patch_create`：生成可審核 patch，不改 active config。
- `config_patch_apply`：套用指定 `patch_id`，寫入 durable config 並重建 generated config。
- `config_render`：從 durable state 重新渲染 `generated/mihomo.yaml`。

服務場景：這是現在的「patch-first」模型。Agent 先構建候選變更，再生成 patch 給用戶/自己審核，最後 apply，不直接亂改 active config。

**5. 路由解釋與可理解性**

- `routing_explain`：解釋某個服務、域名、pack、policy group、出口目前怎麼路由。
- `runtime_profile_status`：看當前 Meta/Smart、normal/router 等 runtime profile。
- `runtime_status`：看 Mihomo 是否運行、PID、controller/UI endpoint。
- `router_takeover_status`：看 OpenWrt firewall/nft/DNS hijack/fwmark/TUN 接管狀態。

服務場景：回答「現在 Telegram 為什麼不走代理」、「Steam 最終落到哪個出口」、「是否已接管路由器網路」。

**6. 執行型工具**

- `run_runtime`：啟動 Mihomo。
- `restart_runtime`：stop/start/status，假設配置已由 `config_patch_apply` 或 `doctor` 驗證，適合 Agent 依賴代理路徑時使用。
- `stop_runtime`：停止 Mihomo；如果 router takeover 生效，預設拒絕，避免斷網。
- `router_takeover_apply`：套用 localClash 管理的 OpenWrt runtime 接管規則。
- `router_takeover_stop`：撤銷 localClash 管理的接管規則，不停止 Mihomo。

服務場景：真正改變運行狀態或路由器網路狀態，所以是 `confirm_required`。這組現在也會輸出階段性 task log，Agent 可以追 `stop -> start -> status` 或 takeover script/verify 的進度。

**推薦 Agent 流程**

1. `tools_list` + `environment_inspect`
2. `subscriptions_status` / `config_status` / `runtime_status`
3. 需要規則證據時走 `packs_list` / `pack_rules_prefetch` / `pack_rules_query`，pack 參數使用 `{source, pack}`
4. 需要修改配置時走 `proxy_group_build` / `policy_group_build` / `custom_rules_build`
5. 用 `config_patch_create` 產生 patch
6. 用 `config_patch_apply` 套用
7. 用 `config_render` 或直接由 apply 產生 generated config
8. 經用戶確認後 `restart_runtime`
9. 路由器接管只在明確確認後 `router_takeover_apply`

長任務現在的觀測入口是：工具返回 `task_id`、`log_file`、`status_file`，Agent 應該用 `nl_file` 持續讀 log，而不是等待 MCP 一次性返回。
