# 更新日誌

這份文件記錄 localClash 產品層的使用者可見變更。它不是 GitHub
Release 頁面的替代品；Release 頁面仍是下載二進位文件、OpenWrt
package、checksum 和 manifest 的來源。

localClash 有兩條獨立的發佈渠道：

- **localClash Core**：Go runtime、MCP/CLI、release manifest、base assets。
  由 [qoli/localClash](https://github.com/qoli/localClash/releases) 發佈。
- **localclash-luci**：OpenWrt LuCI 頁面、rpcd helper、ACL、menu、IPK/APK。
  由 [qoli/localclash-luci](https://github.com/qoli/localclash-luci/releases) 發佈。

Core 發佈不一定需要 LuCI package 發佈。已安裝最新 LuCI package 的路由器，
可以在 LuCI 頁面裡直接更新 Core。

## Unreleased

- 暫無。

## 目前最新版本

| 渠道 | 最新版本 | 發佈時間 |
| --- | --- | --- |
| localClash Core | [v0.1.62](https://github.com/qoli/localClash/releases/tag/v0.1.62) | 2026-08-26 UTC+8 |
| localclash-luci | [v0.1.0-53](https://github.com/qoli/localclash-luci/releases/tag/v0.1.0-53) | 2026-08-21 UTC+8 |

## 2026-08-26

### localClash Core v0.1.62

Changes:

- 內建 router profile 不再發現、競速或注入 WAN resolver；加密 DNS 基線改為
  AliDNS DoH 主查詢與經 `DNSProxy` 的 lazy Google DoH fallback，污染判斷只保留
  明確的 bogon／reserved CIDR，不再使用粗粒度 `geoip-code: CN`。
- Core 改為只消費有版本與有效期的 `nameserver_policy` overlay，拒絕覆蓋既有
  policy，並把完整生成配置交給 `mihomo -t`。STUN、HTTP provider、WAN、country、
  ECS 與候選評分全部留在獨立 dnsqualify，不再進入 Core interface。
- 缺少或過期的 overlay 會明確停用這項可選最佳化並保留加密基線；格式損壞或
  policy 衝突仍會停止 render，不會產生看似有效的替代配置。

Release:

- [qoli/localClash v0.1.62](https://github.com/qoli/localClash/releases/tag/v0.1.62)

Verification:

- Core 與 dnsqualify 的 `go test ./...`、`go vet ./...`，以及 LuCI rpcd／shell／
  JavaScript 測試與 IPK／APK build 全部通過。
- iStoreOS 24.10.8 VM 實測中國境內 STUN 與阻斷 UDP/3478 後的 `ipapi.is` JSON
  路徑，兩者均取得 `14.216.16.0/24`；overlay 經 Core 合併後由 Mihomo v1.19.30
  `-t` 驗證通過。測試後已回復 secure baseline，未啟動 Mihomo runtime。

## 2026-08-21

### localClash Core v0.1.61

Changes:

- 網絡接管改用 localClash 獨立擁有的 `fwmark`、route table 與 rule priority，
  避免 OpenClash 停止或更新時清理共用路由身份，導致 UDP／ICMP 接管失效。
- 首次更新會在確認 localClash 運行狀態與 nft ownership 後遷移舊路由身份；證據
  不完整時明確拒絕遷移，不會猜測所有權或靜默清理。

Release:

- [qoli/localClash v0.1.61](https://github.com/qoli/localClash/releases/tag/v0.1.61)

Verification:

- `go test ./...`、`go vet ./...`、release broadcast 回歸測試與本地 release
  asset build 通過；Release workflow
  [32391198601](https://github.com/qoli/localClash/actions/runs/32391198601) 成功。
- 7 個公開資產重新下載後，三份 SHA-256、amd64／arm64 binary 架構與 manifest
  `v0.1.61` 均通過驗證。
- iStoreOS 24.10.8 VM 以 OpenClash v0.47.088 重現舊路由身份被 stop 清理；遷移
  至新身份後再次執行相同 stop，接管仍為 `effective=true`，標記流量仍指向 TUN。

### localclash-luci v0.1.0-53

Changes:

- 接管診斷快照改讀 localClash 新的獨立 route table，並保留舊身份關鍵字供遷移
  問題回報使用。
- iStoreOS 離線 bundle 的 Core 固定版本由 `v0.1.59` 更新為 `v0.1.61`。

Release:

- [qoli/localclash-luci v0.1.0-53](https://github.com/qoli/localclash-luci/releases/tag/v0.1.0-53)

Verification:

- main CI [32391713906](https://github.com/qoli/localclash-luci/actions/runs/32391713906)
  與 Release workflow
  [32391901277](https://github.com/qoli/localclash-luci/actions/runs/32391901277) 全部成功。
- 13 個公開資產重新下載後，精確數量與全部 SHA-256 均通過；x86_64／aarch64
  iStore `.run` 亦通過 `--info`、`--list`、`--check` 與 `--noexec` 驗證。

## 2026-08-20

### localClash Core v0.1.60

Changes:

- 預設策略新增 `📥 大模型下载` 業務分組，以 `🌐 全球直连` 為預設出口，並保留
  自動選擇、美國及其他地區出口供下載速度或可達性異常時切換。
- 使用精確 domain 規則覆蓋 Hugging Face Xet／CAS／CDN、ModelScope CDN 與
  Ollama registry；不以整個 AI 平台或共用雲端儲存 suffix 分流，避免網站、API
  及無關下載被錯誤納入。

Release:

- [qoli/localClash v0.1.60](https://github.com/qoli/localClash/releases/tag/v0.1.60)

Verification:

- `go test ./...`、`go vet ./...`、release broadcast 回歸測試及本地 release
  asset build 通過；Release workflow
  [32327279549](https://github.com/qoli/localClash/actions/runs/32327279549) 成功。
- 7 個公開資產及三份 SHA-256 全部通過；兩架構 binary 分別確認為
  x86-64／ARM64，manifest 版本與下載 URL 均為 `v0.1.60`。
- 從公開 `localclash-base-assets.tar.gz` 解包讀回預設模板，確認新分組第一出口為
  `🌐 全球直连`，並包含 `us.aws.cdn.hf.co` 等精確下載域名。

### localclash-luci v0.1.0-52

Changes:

- 重新設計概述頁的資訊與視覺層級，以整體狀態面板、語義色彩及集中式主要操作，
  讓運行、接管與異常狀態更容易辨識。
- 精簡狀態摘要、更新區及操作按鈕；首次設定的 Core 與配置方案改為語義明確的
  單選項，並改善桌面與移動端的響應式排列。
- Agent／MCP 接入與 Issue 回報改為可折疊的幫助與診斷區，並修正展開項目標題、
  輔助文字及切換圖示在桌面與移動端的對齊。

Release:

- [qoli/localclash-luci v0.1.0-52](https://github.com/qoli/localclash-luci/releases/tag/v0.1.0-52)

Verification:

- 本地 JavaScript、Takeover Issue、dnsqualify、全部 rpcd/hotplug 測試及完整
  release asset build 通過；main CI
  [32346112929](https://github.com/qoli/localclash-luci/actions/runs/32346112929) 與
  Release workflow
  [32346275948](https://github.com/qoli/localclash-luci/actions/runs/32346275948) 全部成功。
- 13 個公開資產重新下載後，精確數量與全部 SHA-256 均通過；x86_64／aarch64
  iStore `.run` 亦通過 `--info`、`--list`、`--check` 與 `--noexec` 驗證。
- 在 OpenWrt Bootstrap 與 iStoreOS 24.10.8 x86_64／ArgonTheme 2.2.12.3 的
  桌面及 390×844 移動端驗證：頁面與 UBus 請求正常、console 無錯誤，表格和
  操作正確重排，沒有頁面級水平溢出或 localClash 對比度失敗。

## 2026-08-18

### localClash Core v0.1.59

Changes:

- 網絡接管狀態改為單次、最長 8 秒的精確觀測；只讀指定 nft chain，不再掃描
  完整 ruleset，並回傳耗時。
- runtime 停止流程會把已自行結束的受管程序視為已停止，避免訊號競態誤報失敗。

Release:

- [qoli/localClash v0.1.59](https://github.com/qoli/localClash/releases/tag/v0.1.59)

Verification:

- `go test ./...`、`go vet ./...` 與重複 runtime stop 回歸測試通過；Release
  workflow [32045482286](https://github.com/qoli/localClash/actions/runs/32045482286)
  成功。
- 7 個公開資產及 SHA-256 全部通過；兩架構 binary 分別確認為 x86-64／ARM64，
  manifest 版本與資產 checksum 均為 `v0.1.59`。
- 真實 OpenWrt 與 iStoreOS VM 的接管查詢各完成 20 次順序及 8 路併發 Arc
  驗收，全部成功、沒有 XHR timeout。

### localclash-luci v0.1.0-51

Changes:

- 接管查詢失敗或逾時時明確顯示「實際接管狀態未知」，不再沿用舊狀態。
- Core 與 LuCI 更新檢查加入各自的 single-flight 鎖；重疊請求會立即明確回報
  `*_update_check_in_progress`，不再堆積並耗盡 rpcd／Web worker。
- iStoreOS 離線 bundle 的 Core 固定版本由 `v0.1.51` 更新為 `v0.1.59`。

Release:

- [qoli/localclash-luci v0.1.0-51](https://github.com/qoli/localclash-luci/releases/tag/v0.1.0-51)

Verification:

- main CI [32045815676](https://github.com/qoli/localclash-luci/actions/runs/32045815676)
  與 Release workflow
  [32045946477](https://github.com/qoli/localclash-luci/actions/runs/32045946477) 全部成功。
- 13 個公開資產及 SHA-256 全部通過；x86_64／aarch64 iStore `.run` 均通過
  `--info`、`--list`、`--check` 與 `--noexec` 驗證。
- 真實 OpenWrt 的 20 次接管查詢中位數 531 ms、P95 620 ms；iStoreOS VM
  中位數 501 ms、P95 524 ms，兩邊均為 20/20 成功且沒有接管 XHR timeout。

## 2026-08-17

### localclash-luci v0.1.0-50

Changes:

- Takeover Issue 區新增「我已登入 GitHub」確認；「到 GitHub 回報 Issue」按鈕
  預設停用，只有勾選後才可操作，取消勾選會立即再次停用。
- click handler 會再次驗證 checkbox，診斷生成期間也會鎖住 checkbox；完成或
  失敗後仍依當前勾選狀態恢復按鈕，避免繞過登入提示或被 busy 狀態誤啟用。

Release:

- [qoli/localclash-luci v0.1.0-50](https://github.com/qoli/localclash-luci/releases/tag/v0.1.0-50)

Verification:

- 本地 JavaScript、Takeover Issue 報告與全部 rpcd/hotplug 測試通過；main CI
  [31991023311](https://github.com/qoli/localclash-luci/actions/runs/31991023311)
  及 Release workflow
  [31991123880](https://github.com/qoli/localclash-luci/actions/runs/31991123880)
  全部成功。
- 13 個公開資產重新下載後，精確檔名與全部 SHA-256 均通過；x86_64／aarch64
  iStore `.run` 亦通過 `--info`、`--list`、`--check` 與 `--noexec` 解包驗證。
- 正式 IPK 解包回讀已確認包含 checkbox、登入提示及預設 disabled gate；本版
  尚未部署到真實路由器。

### localclash-luci v0.1.0-49

Changes:

- 修正 Takeover Issue 報告模組的 LuCI loader export 契約；更新後「概述」頁可
  正常顯示最底部的 Issue 回報區，不再因 `invalid constructor` 中止 render。
- 新增模組型態回歸測試，除了驗證診斷內容、隱私 allowlist 與 GitHub URL
  長度，也會確認資源輸出為 LuCI 可實例化的 constructor。

Release:

- [qoli/localclash-luci v0.1.0-49](https://github.com/qoli/localclash-luci/releases/tag/v0.1.0-49)

Verification:

- 本地 JavaScript、報告模組、rpcd/hotplug 及 release helper 測試通過；main CI
  [31989973864](https://github.com/qoli/localclash-luci/actions/runs/31989973864)
  及 Release workflow
  [31990066454](https://github.com/qoli/localclash-luci/actions/runs/31990066454)
  全部成功。
- 13 個公開資產重新下載後，精確檔名與全部 SHA-256 均通過；x86_64／aarch64
  iStore `.run` 亦通過 `--info`、`--list`、`--check` 與 `--noexec` 解包驗證。
- 真實 `opkg` 路由器由 `0.1.0-48` 更新至 `0.1.0-49` 後，package metadata、
  rpcd 與實際模組內容回讀正確；Mihomo runtime 保持運行。Arc hard reload 後
  概述頁正常 render，Takeover Issue 區與兩個回報按鈕均可見，console 不再出現
  `factory yields invalid constructor`。

### localclash-luci v0.1.0-48

Changes:

- 新增持久且有界的網絡接管事件日誌，保留 boot、觸發來源、hotplug 恢復、
  apply／stop 結果及真實退出碼；並修正原先可能把 Core 失敗遮成成功的 shell
  狀態處理。
- 「查看接管日誌」會同時收集 IPv4／IPv6 policy route、`utun`、fw4/nft 規則、
  7874／7892／9090 listener、近期 firewall/netifd 事件與 Core status，缺少材料時
  明確標示報告不完整。
- LuCI 概述頁最底部新增 Takeover Issue 回報區，可複製完整 Markdown 報告，或
  打開 GitHub New Issue 並預填問題模板與最近診斷；GitHub 版資料採 allowlist
  並遮罩網絡地址，避免貼出本地 DNS 或 domain。

Release:

- [qoli/localclash-luci v0.1.0-48](https://github.com/qoli/localclash-luci/releases/tag/v0.1.0-48)

Verification:

- 本地 JavaScript、rpcd/hotplug、DNS qualify、release asset 與離線 installer
  測試通過；main CI
  [31988538858](https://github.com/qoli/localclash-luci/actions/runs/31988538858)
  及 Release workflow
  [31988622872](https://github.com/qoli/localclash-luci/actions/runs/31988622872)
  全部成功。
- 13 個公開資產重新下載後，精確檔名與全部 SHA-256 均通過；x86_64／aarch64
  iStore `.run` 亦通過 `--info`、`--list`、`--check` 與 `--noexec` 解包驗證。
- 本版未在真實路由器部署或重現單獨 `fw4 reload`；其修復觸發仍依賴 WAN
  `ifup`／`ifupdate` 或人工重新應用接管，本次重點是讓失效可追蹤及可回報。

## 2026-08-15

### localClash Core v0.1.57

Changes:

- `ChatGPT-available` 改用 `ab.chatgpt.com/v1/initialize` Statsig 探測，驗證
  HTTP 200、Brotli、JSON 及服務地區。
- 16 個工作節點只重試失敗線路並串流解析；舊 mobile snapshot 會作廢重測。
- MCP 先 render candidate、執行 `mihomo -t`，通過才 promote snapshot 與配置。
- 訂閱完整下載及 Range recovery 統一固定 HTTP/1.1，不影響其他 HTTP 流程。

Release:

- [qoli/localClash v0.1.57](https://github.com/qoli/localClash/releases/tag/v0.1.57)

Release assets:

- `localclash-linux-amd64`
- `localclash-linux-arm64`
- `localclash-base-assets.tar.gz`
- `localclash-release-manifest.json`
- 對應的 `.sha256` checksum 文件

Verification:

- 本地 `go test ./...`、`go vet ./...`、subscription race／HTTP 協議回歸測試
  及 release broadcast 測試通過。
- 隔離的 x86_64 OpenWrt 測試環境在沒有 `GODEBUG` 的情況下，以 HTTP/1.1
  完成兩個真實訂閱來源、102 個節點的刷新；Statsig 對 76 條去重線路完成探測，
  50 個節點進入 `ChatGPT-available`，candidate config、capability snapshot 與
  正式配置均通過驗證並完成 promotion。
- 測試 runtime 實際載入的 50 個 `ChatGPT-available` 成員與 v5 snapshot
  完全一致，`DNSProxy` 亦正確指向 `⚡ 自动选择`；驗收後 runtime 已停止並恢復
  原測試狀態。
- GitHub Release workflow
  [31871304134](https://github.com/qoli/localClash/actions/runs/31871304134) 的
  Linux tests、兩架構 binary、base assets 及 Release 建立全部成功。
- 7 個公開資產重新下載後，精確檔名與全部 SHA-256 均通過；兩個靜態 Linux
  binaries 分別為 x86-64／ARM64，均內嵌 `v0.1.57` 與發版 commit
  `7b05bad`。manifest 宣告 `v0.1.57`，所有 URL、檔案大小及 checksum 均指向
  並符合同版本；base assets 內的預設 template 使用
  `openai.chatgpt.statsig.v1`。annotated tag、發佈時的遠端 `main` 與發版 commit
  同為 `7b05bad`。

### localClash Core v0.1.56

Changes:

- `ChatGPT-available` 在 Smart Core 下新增組別專屬的地區標籤與權重：正常情況
  依序偏好美國、日本、新加坡、台灣及其他節點；若較低順位節點的實際品質明顯
  較好，Smart 仍可讓它勝出，而不是形成固定 fallback 鏈。
- 新增 typed `smart_priority` 產品設定與 MCP `proxy_group_build` 支援；localClash
  會驗證標籤、正數權重及 proxy-name pattern，再安全轉譯成 Mihomo Alpha 的
  `policy-priority`，避免使用者直接維護容易出錯的分隔字串。
- 權重只套用到聲明它的 proxy group，不會污染其他 Smart／自動組；Meta Core
  仍保留 `url-test` 行為且不輸出 Smart-only 欄位。同步修正 proxy group 經
  plan summary round-trip 時可能遺失 `optional` 的問題。

Release:

- [qoli/localClash v0.1.56](https://github.com/qoli/localClash/releases/tag/v0.1.56)

Release assets:

- `localclash-linux-amd64`
- `localclash-linux-arm64`
- `localclash-base-assets.tar.gz`
- `localclash-release-manifest.json`
- 對應的 `.sha256` checksum 文件

Verification:

- 本地 `go test ./...`、`go vet ./...`、release broadcast 回歸測試、策略模板
  JSON 及 diff 靜態檢查通過。
- 測試覆蓋 Smart Core group-scoped priority、其他 Smart 組不繼承權重、Meta Core
  不輸出 Smart-only 欄位、MCP／plan round-trip，以及 regex 與 Mihomo delimiter
  escaping 的錯誤邊界。
- GitHub Release workflow
  [31865994731](https://github.com/qoli/localClash/actions/runs/31865994731) 的
  Linux tests、兩架構 binary、base assets 及 Release 建立全部成功。
- 7 個公開資產重新下載後，精確檔名與全部 SHA-256 均通過；兩個靜態 Linux
  binaries 分別為 x86-64／ARM64，均內嵌 `v0.1.56` 與發版 commit
  `a8f22c3`。manifest 宣告 `v0.1.56`，所有 URL、檔案大小及 checksum 均指向
  並符合同版本；base assets 內亦包含美國、日本、新加坡、台灣及其他的完整
  `ChatGPT-available` Smart priority。annotated tag、發佈時的遠端 `main` 與
  發版 commit 同為 `a8f22c3`。

### localClash Core v0.1.55

Changes:

- 修正 LuCI「一鍵更新」在訂閱刷新後可能因 `ChatGPT-available` 能力尚未解析而
  中止的問題；產品 CLI 現在會先完成能力探測，再生成配置。
- CLI 與 MCP 共用同一套 ChatGPT 能力探測及 snapshot 流程；配置重新生成時會
  讀取已驗證、保留順序的合格節點，不會因 patch registry 重新編譯而遺失結果。
- 沒有節點通過可選能力時，會安全生成空的能力出口；缺少、舊版、格式錯誤或
  不支援的 snapshot，以及必需能力沒有合格節點時，仍會明確失敗。
- 訂閱刷新加入結構化 `capability_refresh` 階段日誌，方便從 LuCI 更新記錄追蹤
  探測開始、完成及失敗原因。

Release:

- [qoli/localClash v0.1.55](https://github.com/qoli/localClash/releases/tag/v0.1.55)

Release assets:

- `localclash-linux-amd64`
- `localclash-linux-arm64`
- `localclash-base-assets.tar.gz`
- `localclash-release-manifest.json`
- 對應的 `.sha256` checksum 文件

Verification:

- 本地 `go test ./...`、`go vet ./...`、release broadcast 回歸測試與 LuCI
  一鍵更新 rpcd contract 測試通過。
- 隔離的 x86_64 OpenWrt 測試環境以實際產品 CLI 完成訂閱刷新、能力探測、v4
  snapshot、patch registry 配置生成及 Mihomo config-test；測試代理不可用而
  可選能力集合為空時仍成功生成有效配置。測試未替換已安裝 Core，也未重啟
  現有 Mihomo runtime。
- GitHub Release workflow
  [31833356846](https://github.com/qoli/localClash/actions/runs/31833356846) 的
  Linux tests、兩架構 binary、base assets 及 Release 建立全部成功。
- 7 個公開資產重新下載後，精確檔名與全部 SHA-256 均通過；兩個靜態 Linux
  binaries 分別為 x86-64／ARM64，均內嵌 `v0.1.55` 與發版 commit
  `04f9487`。manifest 宣告 `v0.1.55`，所有 URL、檔案大小及 checksum 均指向
  並符合同版本；annotated tag、發佈時的遠端 `main` 與發版 commit 同為
  `04f9487`，其後僅新增本次驗證記錄。

### localClash Core v0.1.54

Changes:

- 新增 `ChatGPT-available` 服務能力出口：訂閱更新會透過隔離的臨時 Mihomo，
  同時驗證 ChatGPT iOS 與 Android 根端點的 IP eligibility fingerprint，只把
  通過兩端驗證的節點放入自動選擇組。
- 能力驗證加入有界並行、重試、連續失敗遲滯與既有合格集合突然歸零保護；暫時
  網絡故障不會立刻移除已合格節點，也不會在整組異常崩塌時覆寫既有 snapshot
  或生成配置。明確的 ISP／地區拒絕仍會立即移除節點。
- `🤖 ChatGPT` 保持美國地區出口為預設，新的能力篩選出口放在最後作為 opt-in
  選項，不會自動改變既有使用者的預設路由。
- 多訂閱合併為節點名稱加入來源前綴時，現在同步重寫來源內的
  `dialer-proxy` 引用；若同名節點使引用產生歧義，會明確失敗而不是選擇不確定
  的出口。
- 路由器部署腳本只同步 release 實際提供的 `policy-templates` 與
  `rule-sources` base assets，並在部署前驗證兩個來源目錄存在。

Release:

- [qoli/localClash v0.1.54](https://github.com/qoli/localClash/releases/tag/v0.1.54)

Release assets:

- `localclash-linux-amd64`
- `localclash-linux-arm64`
- `localclash-base-assets.tar.gz`
- `localclash-release-manifest.json`
- 對應的 `.sha256` checksum 文件

Verification:

- 本地 `go test ./...`、`go vet ./...` 與 release broadcast 回歸測試通過。
- ChatGPT capability 的單元及 MCP 整合測試覆蓋兩端 eligibility fingerprint、
  bounded concurrency、重試、明確拒絕、遲滯、集合崩塌保護、snapshot 保密與
  生成 proxy group；多訂閱測試覆蓋 `dialer-proxy` 重寫及歧義拒絕。
- GitHub Release workflow
  [31831309856](https://github.com/qoli/localClash/actions/runs/31831309856) 的
  Linux tests、兩架構 binary、base assets 及 Release 建立全部成功。
- 7 個公開資產重新下載後，精確檔名與全部 SHA-256 均通過；兩個靜態 Linux
  binaries 分別為 x86-64／ARM64，均內嵌 `v0.1.54` 與發版 commit
  `75af481`。manifest 宣告 `v0.1.54`，所有 URL、檔案大小及 checksum 均指向
  並符合同版本；base assets 內的預設 template 亦包含
  `openai.chatgpt.mobile.v1`。annotated tag、發佈時的遠端 `main` 與發版
  commit 同為 `75af481`；其後僅新增本次驗證記錄。

## 2026-08-14

### localClash Core v0.1.53

Changes:

- 訂閱 Range 分段恢復現在明確使用 HTTP/1.1，不再繼承初始請求的 HTTP/2
  ALPN 協商；遇到部分上游的 HTTP/2 中斷時，後續分段可以穩定完成恢復。
- 訂閱初始及分段請求的 transport error 會維持遮罩 URL，不會把完整訂閱 URI、巢狀
  URL 或 token 寫入錯誤日誌。

Release:

- [qoli/localClash v0.1.53](https://github.com/qoli/localClash/releases/tag/v0.1.53)

Release assets:

- `localclash-linux-amd64`
- `localclash-linux-arm64`
- `localclash-base-assets.tar.gz`
- `localclash-release-manifest.json`
- 對應的 `.sha256` checksum 文件

Verification:

- 本地 `go test ./...`、`go vet ./...` 與 release broadcast 回歸測試通過。
- x86_64 OpenWrt 測試環境中，初始 HTTP/2 中斷後以 8 個 HTTP/1.1 Range 分段恢復
  457,685 bytes，完成 2 次邊界驗證；訂閱解析出 2 proxies、27 groups、10,964
  rules，成功生成配置並通過 Mihomo config-test。transport error 日誌未洩漏完整
  URI、巢狀 URL 或 token。
- GitHub Release workflow
  [31781914814](https://github.com/qoli/localClash/actions/runs/31781914814) 的
  Linux tests、兩架構 binary、base assets 及 Release 建立全部成功。
- 7 個公開資產重新下載後，精確檔名與全部 SHA-256 均通過；manifest 宣告
  `v0.1.53`，所有下載 URL、檔案大小及 checksum 均指向並符合同版本。annotated
  tag、發佈時的遠端 `main` 與發佈 commit 同為 `7802a8d`；其後僅新增本次驗證
  記錄。

### localClash Core v0.1.52

Changes:

- `🧠 AI` 策略新增 `🌐 全球直连` 作為可選出口，同時保留 `⚡ 自动选择`
  為預設選項，不改變現有使用者的預設路由行為。
- 訂閱保存與刷新現在輸出可直接分享的結構化完整日誌，逐階段記錄來源選擇、
  HTTP 回應、內容讀取、解析、artifact 寫入及合併結果。
- 訂閱錯誤與日誌使用兩位數顯示 ID 對應遮罩 URL，加入 HTTP 狀態、協議、
  長度及安全的錯誤回應摘要，不再暴露內部 source ID 或訂閱密鑰。
- 上游在成功回應中途斷流時，可明確進入 HTTP/1.1 Range 分段恢復；系統會驗證
  總長、每段位元組數、重疊內容及首尾重讀結果，任何不一致都會失敗且不寫入
  部分或舊 artifact。

Release:

- [qoli/localClash v0.1.52](https://github.com/qoli/localClash/releases/tag/v0.1.52)

Release assets:

- `localclash-linux-amd64`
- `localclash-linux-arm64`
- `localclash-base-assets.tar.gz`
- `localclash-release-manifest.json`
- 對應的 `.sha256` checksum 文件

Verification:

- 本地 `go test ./...`、`go vet ./...` 與 release broadcast 回歸測試通過。
- x86_64 OpenWrt 測試環境的現有 dler 訂閱完整下載、解析及 artifact 合併成功；
  隔離短讀驗收則從 4,096 bytes 的 `unexpected EOF` 經 3 個 Range 分段與 2 次
  邊界重讀恢復 163,809 bytes，完成 1,800 個節點解析，MCP 與 Mihomo 保持健康。
- GitHub Release workflow
  [31778549896](https://github.com/qoli/localClash/actions/runs/31778549896)
  的 Linux tests、兩架構 binary、base assets 及 Release 建立全部成功。
- 7 個公開資產重新下載後，兩個 binaries 與 base assets checksum 均通過；
  manifest 宣告 `v0.1.52` 且所有下載 URL 指向同版本，base assets 亦確認
  `🧠 AI` 保持 `⚡ 自动选择` 為首選並加入 `🌐 全球直连`；tag、遠端 `main`
  與發佈 commit 在發佈時同為 `3512734`。

### localclash-luci v0.1.0-47

Changes:

- LuCI 自動更新讀取 Release、下載 checksum、下載及校驗安裝包遇到暫時失敗時，
  每個階段最多嘗試 3 次並記錄結果；套件安裝、helper 接棒和服務操作仍明確
  失敗，不會重複執行非冪等步驟。
- GitHub Release 頁新增面向普通使用者的下載指南，按 `opkg`、`apk` 和 iStoreOS
  架構直接選擇正確安裝檔案。

Release:

- [qoli/localclash-luci v0.1.0-47](https://github.com/qoli/localclash-luci/releases/tag/v0.1.0-47)

Release assets:

- `luci-app-localclash_0.1.0-47_all.ipk` 及 SHA-256
- `luci-app-localclash-0.1.0-r47.apk` 及 SHA-256
- `dnsqualify-linux-amd64`、`dnsqualify-linux-arm64` 及 SHA-256
- `dnsqualify-release-manifest.json`
- `localclash-istore-v0.1.0-47-x86_64.run` 及 SHA-256
- `localclash-istore-v0.1.0-47-aarch64.run` 及 SHA-256

Verification:

- Main CI
  [31779481727](https://github.com/qoli/localclash-luci/actions/runs/31779481727)
  與 tag Release CI
  [31779593520](https://github.com/qoli/localclash-luci/actions/runs/31779593520)
  均通過來源釘選、dnsqualify test/vet、全部 rpcd 測試、完整打包、離線安裝與
  Release 發佈。
- 13 個公開資產重新下載後，精確檔名與全部 SHA-256 均通過；tag、遠端 `main`
  與發佈 commit 同為 `21260e4`。
- 兩個 `.run` 均通過 Makeself `--info`、`--list`、`--check` 及 `--noexec`；
  這些是 archive 驗證，未據此宣稱新增真實路由器驗收。

## 2026-08-12

### localclash-luci v0.1.0-46

Changes:

- 唯一公開源碼：[`qoli/dnsqualify`](https://github.com/qoli/dnsqualify) 成為唯一源碼，
  LuCI 移除內嵌副本。
- CI 來源釘選：Main／Release CI 鎖定完整 commit；來源、HEAD 或 dirty checkout
  不符即失敗。
- 發布來源證明：Release manifest 記錄來源；兩個 `.run` 內嵌 dnsqualify 與
  公開 asset SHA 一致。

Release:

- [qoli/localclash-luci v0.1.0-46](https://github.com/qoli/localclash-luci/releases/tag/v0.1.0-46)

Release assets:

- `luci-app-localclash_0.1.0-46_all.ipk` 及 SHA-256
- `luci-app-localclash-0.1.0-r46.apk` 及 SHA-256
- `dnsqualify-linux-amd64`、`dnsqualify-linux-arm64` 及 SHA-256
- `dnsqualify-release-manifest.json`
- `localclash-istore-v0.1.0-46-x86_64.run` 及 SHA-256
- `localclash-istore-v0.1.0-46-aarch64.run` 及 SHA-256

Verification:

- Main CI
  [31568307106](https://github.com/qoli/localclash-luci/actions/runs/31568307106)
  與 tag Release CI
  [31568402518](https://github.com/qoli/localclash-luci/actions/runs/31568402518)
  均通過 pinned source checkout、dnsqualify test/vet、rpcd、完整打包、離線安裝
  與 Release 發布。
- 13 個公開資產重新下載後，精確檔名與全部 SHA-256 均通過；tag、遠端 main
  與發布 commit 同為 `20255e6`。
- Manifest 宣告 `qoli/dnsqualify@525bcf8`；amd64 與 arm64 二進制的 Go build
  metadata 亦確認相同 module、完整 VCS revision 及 `vcs.modified=false`，並在
  對應 Linux 容器實際回報 `dnsqualify v0.1.0-46`。
- 兩個 `.run` 均通過 Makeself `--info`、`--list`、`--check`、`--noexec` 與
  payload checksum；其中 dnsqualify SHA 與同架構公開 asset 完全一致。這些是
  archive／Linux runtime 證據，未據此宣稱新增真實路由器驗收。

### localclash-luci v0.1.0-45

Changes:

- LuCI Release 改由 tag 驅動的 GitHub Actions 統一測試、建置及發佈；pull
  request 與 `main` 亦會先產生候選資產，公開 Release 不再依賴本機手工上傳。
- 新增 iStoreOS／opkg 離線 `.run`：x86-64 與 aarch64 各自包含 LuCI IPK、
  固定版本的 Core v0.1.51、對應架構 `dnsqualify`、Core base assets、bundle
  manifest 及 SHA-256，不會在安裝時執行 `opkg update` 或下載 `latest`。
- 離線 installer 會先驗證 root、必要命令、CPU 架構、ELF 架構、全部 checksum
  及 base assets 完整性；失敗時明確終止，不改抓線上替代文件，也不修改訂閱、
  策略選擇或路由器接管狀態。
- 官方 policy／rule 文件採逐文件原子更新，保留使用者額外加入的文件。
- 修正首個 CI 發佈版 v0.1.0-44 的 `dnsqualify` checksum sidecar 帶有 runner
  絕對路徑、下載後無法直接校驗的問題；請使用 v0.1.0-45。

Release:

- [qoli/localclash-luci v0.1.0-45](https://github.com/qoli/localclash-luci/releases/tag/v0.1.0-45)

Release assets:

- `luci-app-localclash_0.1.0-45_all.ipk` 及 SHA-256
- `luci-app-localclash-0.1.0-r45.apk` 及 SHA-256
- `dnsqualify-linux-amd64`、`dnsqualify-linux-arm64` 及 SHA-256
- `dnsqualify-release-manifest.json`
- `localclash-istore-v0.1.0-45-x86_64.run` 及 SHA-256
- `localclash-istore-v0.1.0-45-aarch64.run` 及 SHA-256

Verification:

- Main CI
  [31566502248](https://github.com/qoli/localclash-luci/actions/runs/31566502248)
  與 tag Release CI
  [31566588233](https://github.com/qoli/localclash-luci/actions/runs/31566588233)
  的語法、rpcd、dnsqualify、完整打包、離線安裝及 Release 發佈全部成功。
- 13 個公開資產從 GitHub Release 重新下載後，精確檔名清單及全部 portable
  SHA-256 sidecar 均通過；tag `v0.1.0-45` 指向 release commit `8b7ecba`。
- 兩個 `.run` 均通過 Makeself `--info`、`--list`、`--check` 及 `--noexec`，
  並確認包含正確 IPK、Core、dnsqualify、base assets、manifest 與 installer。
- x86-64 bundle 已在完全斷網的 Linux 容器完成安裝；架構不匹配與 payload
  被篡改均會在安裝前拒絕。aarch64 archive 與 ELF 架構已驗證，但尚未宣稱
  真實 aarch64 iStoreOS 路由器驗收完成。

## 2026-08-09

### localClash Core v0.1.51

Changes:

- 修正 v0.1.49 預設策略在刷新訂閱時，`☁️ Cloudflare` 等業務策略指向
  `🌐 全球直连` 會被錯誤判定為非終端出口，導致一鍵更新於配置生成階段失敗。
- policy group 現在可明確引用另一個 policy group；渲染器會一併輸出被引用的
  selector，未知目標、自我引用及多組循環仍會明確拒絕，不會靜默降級。
- release tests 現在隔離其他 package 的臨時 Mihomo 程序，避免把並行測試程序
  誤認成 reset fixture workspace 的 runtime；production reset safety check 不變。

Release:

- [qoli/localClash v0.1.51](https://github.com/qoli/localClash/releases/tag/v0.1.51)

Release assets:

- `localclash-linux-amd64`
- `localclash-linux-arm64`
- `localclash-base-assets.tar.gz`
- `localclash-release-manifest.json`
- 對應的 `.sha256` checksum 文件

Verification:

- 本地 `go test ./...` 與 `go vet ./...` 通過。
- 回歸測試覆蓋 `☁️ Cloudflare → 🌐 全球直连 → DIRECT`、巢狀 selector
  依賴物化，以及 policy-group cycle 的明確失敗。
- GitHub Release workflow
  [31289889150](https://github.com/qoli/localClash/actions/runs/31289889150)
  的 Linux tests、兩架構 binary、base assets 及 Release 建立全部成功。
- 7 個公開資產重新下載後，兩個 binaries 與 base assets checksum 均通過；
  manifest 宣告 `v0.1.51`、所有 URL 均指向同版本，tag 與遠端 `main` 同為
  `24f2b71`。

### localClash Core v0.1.49

Changes:

- `🌐 全球直连` 現在是 Dashboard 可切換的全域策略出口；第一個也是預設出口
  仍為 `DIRECT`，使用者可明確切換到自動或地區代理節點。
- 預設模板的最終 `MATCH` 改為指向 `🌐 全球直连`。預設行為仍保持未分類流量
  直連及遊戲加速器相容性，但不再需要修改模板才能臨時切換全域策略。
- `geolocation-!cn` 的 Dashboard 策略由「漏網之魚」明確改名為
  `🌍 非中國網站`，使規則涵蓋範圍和實際用途一致。
- Core repository 新增官方 `localclash-mcp-route-operator` Codex Skill，要求
  服務路由優先使用專用出口，並以配置意圖、已載入狀態、連線與日誌分層觀測；
  Draft 觸及 shared/default group 時必須停止並等待明確確認。

Release:

- [qoli/localClash v0.1.49](https://github.com/qoli/localClash/releases/tag/v0.1.49)

Release assets:

- `localclash-linux-amd64`
- `localclash-linux-arm64`
- `localclash-base-assets.tar.gz`
- `localclash-release-manifest.json`
- 對應的 `.sha256` checksum 文件

Verification:

- 本地 `go test ./...`、`go vet ./...`、release broadcast 回歸測試及 Skill
  結構驗證通過。
- GitHub Release workflow
  [31279818731](https://github.com/qoli/localClash/actions/runs/31279818731)
  的 Linux tests、兩架構 binary、base assets 及 Release 建立全部成功。
- 7 個公開資產重新下載後，兩個 binaries 與 base assets checksum 均通過；
  manifest 宣告 `v0.1.49` 並指向同版本資產，tar 內亦確認包含新的
  `🌐 全球直连` 與 `🌍 非中國網站` 策略。

### localclash-luci v0.1.0-43

Changes:

- 概覽頁新增「Agent Skill 與 MCP 接入」引導，讓 Codex 同時安裝官方
  `localclash-mcp-route-operator` Skill 並連接真實路由器 MCP。
- Agent 現在會被明確引導為服務、應用或遊戲建立專用策略出口，不再為單一
  服務隨意覆蓋「自動選擇」等共享或預設策略組。
- Draft 一旦觸及 shared/default group 就必須停止套用，先呈現具體組、目前
  行為、影響範圍與專用出口替代方案；只有使用者明確點名確認後才能繼續。
- 接入文本加入配置意圖、Mihomo 已載入狀態、目前連線與限定時間日誌的分層
  觀測說明，並將配置寫入、runtime 載入、服務重啟及路由器接管分開授權。

Release:

- [qoli/localclash-luci v0.1.0-43](https://github.com/qoli/localclash-luci/releases/tag/v0.1.0-43)

Release assets:

- `luci-app-localclash_0.1.0-43_all.ipk` 及 SHA-256
- `luci-app-localclash-0.1.0-r43.apk` 及 SHA-256
- `dnsqualify-linux-amd64`、`dnsqualify-linux-arm64` 及 SHA-256
- `dnsqualify-release-manifest.json`

Verification:

- LuCI JavaScript、rpcd helper shell 語法與 diff 檢查通過。
- IPK、APK、兩架構 dnsqualify 與 checksum 均成功建置及驗證。
- GitHub Release 為非 draft、非 prerelease 且標記 Latest；9 個資產已上傳，
  tag 與遠端 `main` 均指向 `8bab994`。

## 2026-08-01

### localclash-luci v0.1.0-42

Changes:

- `dnsqualify` 現在會在候選準備、每輪 DNS 查詢、服務連通性與速度測量、
  結果選擇及配置寫入時輸出即時進度。
- 長時間測量每 15 秒輸出目前階段與已用時間，避免任務仍在正常執行時看起來
  像卡死。
- 進度只寫入 stderr 並由 LuCI 完整任務日誌收集；stdout 仍只包含最終 JSON，
  不改變既有 rpcd 契約。

Release:

- [qoli/localclash-luci v0.1.0-42](https://github.com/qoli/localclash-luci/releases/tag/v0.1.0-42)

Release assets:

- `luci-app-localclash_0.1.0-42_all.ipk` 及 SHA-256
- `luci-app-localclash-0.1.0-r42.apk` 及 SHA-256
- `dnsqualify-linux-amd64`、`dnsqualify-linux-arm64` 及 SHA-256
- `dnsqualify-release-manifest.json`

Verification:

- dnsqualify Go tests／race tests／vet、rpcd 回歸測試、LuCI JavaScript／shell
  語法、IPK／APK 建置與所有 release asset checksum 均通過。
- ARM64 OpenWrt 預設工作量實測約 42 秒，在第 15 秒與第 30 秒均顯示心跳；
  安裝後 dnsqualify 為 `v0.1.0-42`，Mihomo PID 保持不變，既有 DNS 配置狀態
  亦未被改動。

## 2026-07-31

### localClash Core v0.1.48

Changes:

- `geosite:cn` 優先使用來源可確認且可回應的 WAN DNS；不可用時才以明確原因
  回退 AliDNS。
- Core 嚴格驗證 LuCI 產生的 `dnsqualify.json`；malformed 配置會停止 render，
  不會隱藏回退。
- 最佳化只改變中國大陸服務 DNS；節點域名 DNS 保持獨立。

Release:

- [qoli/localClash v0.1.48](https://github.com/qoli/localClash/releases/tag/v0.1.48)

Release assets:

- `localclash-linux-amd64`
- `localclash-linux-arm64`
- `localclash-base-assets.tar.gz`
- `localclash-release-manifest.json`
- 對應的 `.sha256` checksum 文件

Verification:

- `go test ./...`、`go vet ./...` 與 Linux ARM64 候選建置通過。
- ARM64 OpenWrt 實測由 9 個候選選出 `114DNS UDP`；Core render 明確記錄
  `mode=dnsqualify`，Mihomo 隔離配置測試通過，既有 runtime PID 未改變。

### localclash-luci v0.1.0-41

Changes:

- LuCI 按架構發佈和安裝 `dnsqualify`，驗證 manifest、版本及 SHA-256 後才
  原子替換。
- 只有按下按鈕才會測量；驗證失敗恢復舊配置，生效仍需明確重啟，reset 可回到
  Core 預設 WAN DNS。
- 任務視窗保留並可複製完整日誌；下載失敗會附帶 downloader、host、耗時、
  bytes、exit code 及 DNS 診斷。

Release:

- [qoli/localclash-luci v0.1.0-41](https://github.com/qoli/localclash-luci/releases/tag/v0.1.0-41)

Release assets:

- `luci-app-localclash_0.1.0-41_all.ipk` 及 SHA-256
- `luci-app-localclash-0.1.0-r41.apk` 及 SHA-256
- `dnsqualify-linux-amd64`、`dnsqualify-linux-arm64` 及 SHA-256
- `dnsqualify-release-manifest.json`

Verification:

- 全部 rpcd 回歸測試、LuCI JavaScript／shell 語法、dnsqualify Go tests／vet、
  IPK／APK 與兩架構 release assets 建置及 checksum 均通過。
- ARM64 OpenWrt 已驗證候選 LuCI 安裝、真實測量、Core 契約、reset 回到三個
  WAN resolver，以及全程不自動重啟 Mihomo；失敗保留舊狀態由 rpcd 回歸測試
  覆蓋。

## 2026-07-22

### localClash Core v0.1.47

Changes:

- 預設策略新增 Dashboard 可見的「☁️ Cloudflare」業務群組；已知
  Cloudflare IP 範圍現在預設經「⚡ 自动选择」代理，並保留手動、直連與各地區
  出口覆寫。
- 規則使用 `GEOIP,cloudflare` 並置於終端 `MATCH,DIRECT` 之前，因此裸 IP
  例如 `1.1.1.1` 不再因未知流量的黑名單預設而被強制直連。
- 未加入 `GEOSITE,cloudflare`：Cloudflare 自有網域仍依既有網域策略和直連
  邊界處理，避免把可直連的 Cloudflare DNS／平台服務一概送往代理。

Release:

- [qoli/localClash v0.1.47](https://github.com/qoli/localClash/releases/tag/v0.1.47)

Release assets:

- `localclash-linux-amd64`
- `localclash-linux-arm64`
- `localclash-base-assets.tar.gz`
- `localclash-release-manifest.json`
- 對應的 `.sha256` checksum 文件

Verification:

- 本機 `go test ./...`：430 項測試通過。
- GitHub Release workflow 會在 tag 建立後重新執行 Linux 測試並建置上述資產。

## 2026-07-21

### localclash-luci v0.1.0-40

Changes:

- 「同步最新默认策略（推荐）」勾選時，現在會明確重置整套本地策略 patch，再匯入最新內建預設策略；使用者自訂策略也會被覆蓋。
- 同一 rule pack 的本地覆寫不再會阻斷一鍵更新。取消勾選則繼續保留目前本地策略。
- 概覽頁已說明這個選項的全量覆蓋邊界，避免將其誤認為只更新內建規則。

Release:

- [qoli/localclash-luci v0.1.0-40](https://github.com/qoli/localclash-luci/releases/tag/v0.1.0-40)

Release assets:

- `luci-app-localclash_0.1.0-40_all.ipk`
- `luci-app-localclash_0.1.0-40_all.ipk.sha256`
- `luci-app-localclash-0.1.0-r40.apk`
- `luci-app-localclash-0.1.0-r40.apk.sha256`

Verification:

- localClash 全量測試（430 項）與「完整重置衝突策略」回歸測試通過。
- LuCI 一鍵更新回歸測試、JavaScript 與 rpcd helper 語法檢查，以及 IPK/APK 建置與 SHA-256 校驗通過。

## 2026-07-20

### localClash Core v0.1.46

Changes:

- Core watchdog 現在會在同一次開機內，對意外退出的 Mihomo 進行最多三次
  有界恢復；每次只會啟動已驗證的同一核心與設定，並在 Controller
  `/version` 恢復健康後才確認成功。
- 恢復採用立即、10 秒、30 秒退避；連續耗盡額度會進入
  `latched_failed`，避免崩潰循環。人工 `runtime start/restart` 可解除鎖定，
  持續健康 10 分鐘會重置事故額度。
- boot、core/config hash、驗證證明或程序身分不符時一律停止恢復；既有程序
  Controller 不健康時只記錄，不會由 watchdog 殺死或替換。監督狀態與所有
  決策分別保存在 `runtime-supervision.json` 與 `watchdog.jsonl`。
- 預設路由新增 Syncnext 維護的直連與代理規則，並安排在寬泛中國網域直連
  規則之前，避免已知應用網域被較粗的分類提前攔截。
- 新啟動的 Mihomo 若未能通過 Controller 健康檢查，Core 會清理該次生成的
  PID 並把監督狀態落回 `stopped`，避免留下未受管程序。

Release:

- [qoli/localClash v0.1.46](https://github.com/qoli/localClash/releases/tag/v0.1.46)

Release assets:

- `localclash-linux-amd64`
- `localclash-linux-arm64`
- `localclash-base-assets.tar.gz`
- `localclash-release-manifest.json`
- 對應的 `.sha256` checksum 文件

Verification:

- GitHub Release workflow
  [29726751405](https://github.com/qoli/localClash/actions/runs/29726751405)
  的原生 Linux tests 與 release asset build 均成功。
- 遠端 amd64、arm64 與 base assets checksum 全部通過；manifest 宣告
  `v0.1.46`，並指向同版本的 7 個正式資產。
- 本地 429 項全測試、137 項 race 測試與 `go vet` 通過；隔離 Docker
  OpenWrt 已驗證意外退出恢復、三次額度鎖定及人工 start 解除鎖定。
- `v0.1.45` 僅留下失敗的公開 tag，未建立 GitHub Release；實際發布版本為
  `v0.1.46`，沒有移動或覆寫既有 tag。

## 2026-07-14

### localclash-luci v0.1.0-39

Changes:

- WAN 的 `ifup` / `ifupdate` 連續觸發 fw4 reload 時，接管恢復現在以最後一個
  hotplug event 為準；較早的延遲工作會自行退出，避免過早重新套用接管後又被
  後續 fw4 reload 清除。
- 恢復延遲採用明確的事件 token，只有最後一個事件會執行；
  `LOCALCLASH_RESTORE_DELAY` 若不是非負整數會立即失敗，不會靜默改用其他值。
- localClash Core 已驗證接管重新生效後，rpcd helper 會留下成功日誌，讓 WAN
  波動後的恢復完成狀態可追蹤。
- 新增集中 WAN event 的回歸測試，確認只會在最後事件後執行一次恢復。

Release:

- [qoli/localclash-luci v0.1.0-39](https://github.com/qoli/localclash-luci/releases/tag/v0.1.0-39)

Release assets:

- `luci-app-localclash_0.1.0-39_all.ipk`
- `luci-app-localclash_0.1.0-39_all.ipk.sha256`
- `luci-app-localclash-0.1.0-r39.apk`
- `luci-app-localclash-0.1.0-r39.apk.sha256`

Verification:

- LuCI JavaScript `node --check`、hotplug/rpcd helper shell syntax、
  `scripts/test-rpcd-takeover-restore.sh` 與
  `scripts/test-hotplug-takeover-restore.sh` 均通過。
- IPK/APK 已完成建置與 SHA-256 校驗；GitHub Release `v0.1.0-39` 已標記為
  Latest，tag 指向 package release commit `f81d263`，並包含 4 個資產。

## 2026-07-10

### localclash-luci v0.1.0-38

Changes:

- 新版 helper 同 PID 接棒：LuCI package 更新後，同一個任務 PID 會重新載入
  磁碟上的新版 helper，驗證 lock 與狀態後才繼續更新 Core。
- Core 替換後強制 service restart：完成原子替換後立即透過 procd 啟用新版，
  並同時驗證 `mcp` instance 與 HTTP health；不再用 PID、進程名稱或
  runtime checksum 猜測是否需要重啟。
- 服務生命週期失敗顯式化：service wrapper 改為原子寫入，獨立 LuCI 更新
  與一鍵更新各自只有一個明確的 restart owner；寫入、重啟或 readiness
  失敗都會顯式中止。
- 舊版首次升級需兩步：從 `v0.1.0-37` 或更舊版本升級時，請先執行「檢查
  LuCI 更新」、刷新頁面，再執行一次「一鍵更新」；後續版本會自動完成
  helper 交接。

Release:

- [qoli/localclash-luci v0.1.0-38](https://github.com/qoli/localclash-luci/releases/tag/v0.1.0-38)

Release assets:

- `luci-app-localclash_0.1.0-38_all.ipk`
- `luci-app-localclash_0.1.0-38_all.ipk.sha256`
- `luci-app-localclash-0.1.0-r38.apk`
- `luci-app-localclash-0.1.0-r38.apk.sha256`

Verification:

- GitHub Release `v0.1.0-38` 已標記為 Latest，tag 指向 merge commit
  `0fd5498`，4 個遠端資產 digest 全部與本地建置一致。
- 13 個 rpcd 測試、LuCI JavaScript `node --check`、helper `sh -n`、
  BusyBox ash syntax、IPK/APK build 與 checksum 驗證均通過。

## 2026-07-09

### localClash Core v0.1.44

Changes:

- MCP server 新增 watchdog，會定期檢查 Mihomo runtime log；當
  `mihomo.log` 超過預設 10 MiB 時會截斷並寫入 `watchdog.jsonl`，避免長
  時間運行後 log 無限制膨脹。
- 自動健康檢查預設間隔調整為 60 秒，讓預設策略組的節點檢測更快收斂。
- 新增代理伺服器 DNS 策略文檔，釐清 `proxy-server-nameserver-policy` 的適用情境，方便排查私有代理伺服器網域解析問題。

Release:

- [qoli/localClash v0.1.44](https://github.com/qoli/localClash/releases/tag/v0.1.44)

Release assets:

- `localclash-linux-amd64`
- `localclash-linux-arm64`
- `localclash-base-assets.tar.gz`
- `localclash-release-manifest.json`
- 對應的 `.sha256` checksum 文件

Verification:

- GitHub Release `v0.1.44` 已包含 linux amd64/arm64、base assets、
  release manifest 和 checksum assets。
- Release workflow `29021835403` 已完成，`test` 與 `build-release` job
  均成功。
- 本地驗證通過 `rtk go test ./...`。

## 2026-06-04

### localClash Core v0.1.41-v0.1.43

Changes:

- 訂閱 proxy URI lines 現在可以容忍非 URI 說明行，例如機場輸出的
  `REMARKS=`、`STATUS=` 或其他純文字行；只要後續包含有效的
  proxy URI，就會繼續解析並合併。
- 整包 base64 包裹的 proxy URI 訂閱會先解包再解析；OICS 這類返回
  `REMARKS`、`STATUS` 與 AnyTLS URI lines 的訂閱可以正常匯入。
- 自訂規則新增 `domain_regex`，會渲染成 Mihomo `DOMAIN-REGEX`，適合
  Prime Video 這類有固定結構的 CDN host 變體。

Release:

- [qoli/localClash v0.1.43](https://github.com/qoli/localClash/releases/tag/v0.1.43)
- [qoli/localClash v0.1.42](https://github.com/qoli/localClash/releases/tag/v0.1.42)
- [qoli/localClash v0.1.41](https://github.com/qoli/localClash/releases/tag/v0.1.41)

Release assets:

- `localclash-linux-amd64`
- `localclash-linux-arm64`
- `localclash-base-assets.tar.gz`
- `localclash-release-manifest.json`
- 對應的 `.sha256` checksum 文件

Verification:

- GitHub Release `v0.1.43` 已包含 linux amd64/arm64、base assets、
  release manifest 和 checksum assets。
- Release workflow `26957028277` 已完成，`test` 與 `build-release` job
  均成功。
- Docker OpenWrt 已用 PQJC + OICS 雙訂閱驗證，合併後 224 個 proxies。
- 本地驗證通過 `rtk go test ./internal/subscriptions` 和 `rtk go test ./...`。

### localclash-luci v0.1.0-37

Changes:

- rpcd helper 的 GitHub mirror fallback log 更清楚：直接下載失敗、鏡像
  候選與後續嘗試會更容易在一鍵更新或元件下載問題中追蹤。

Release:

- [qoli/localclash-luci v0.1.0-37](https://github.com/qoli/localclash-luci/releases/tag/v0.1.0-37)

Release assets:

- `luci-app-localclash_0.1.0-37_all.ipk`
- `luci-app-localclash_0.1.0-37_all.ipk.sha256`
- `luci-app-localclash-0.1.0-r37.apk`
- `luci-app-localclash-0.1.0-r37.apk.sha256`

Verification:

- GitHub Release `v0.1.0-37` 已標記為 Latest，且包含 IPK、APK 與兩者
  checksum sidecar。
- 發佈前已通過 LuCI JavaScript `node --check`、rpcd helper `sh -n`、
  `git diff --check`、IPK/APK build 與 sha256 校驗。

### localClash Core v0.1.39-v0.1.40

Changes:

- 預設策略補上 BT/PT 下載分流：`category-pt`、public tracker 與常見下載
  相關規則會進入可在 Dashboard 調整的 `BT/PT 下載` 策略組，預設直連，
  避免把大流量下載錯送進代理。
- 預設策略模板可以被 LuCI 一鍵更新同步到最新版本；同步新版預設規則時，
  使用者自訂規則仍保留，只有內建預設模板會跟著新版修正。
- 產品 CLI 接受 LuCI 一鍵更新使用的 runtime restart strategy，讓最後的
  runtime 切換可以明確走 `process_restart`，減少長時間不透明等待。

Release:

- [qoli/localClash v0.1.39](https://github.com/qoli/localClash/releases/tag/v0.1.39)
- [qoli/localClash v0.1.40](https://github.com/qoli/localClash/releases/tag/v0.1.40)

Release assets:

- `localclash-linux-amd64`
- `localclash-linux-arm64`
- `localclash-base-assets.tar.gz`
- `localclash-release-manifest.json`
- 對應的 `.sha256` checksum 文件

Verification:

- GitHub Release `v0.1.40` 已包含 linux amd64/arm64、base assets、
  release manifest 和 checksum assets。
- 真實路由器的一鍵更新流程已透過 LuCI 調用 `runtime restart --strategy
  process_restart`，並在完成後保持 runtime running 與 router takeover
  effective。

### localclash-luci v0.1.0-31-v0.1.0-36

Changes:

- `一鍵更新` 移到概覽頁作為唯一入口，進階頁保留元件級維護，避免同一個
  小白 flow 在兩個位置重複維護。
- 新增「同步最新默认策略」勾選項，預設適合一般使用者；偏好寫在路由器
  檔案系統，不依賴瀏覽器 localStorage。
- 修正 IPK 更新造成 LuCI logout / rpcd reload 時的任務體驗：仍在執行的
  後台任務會在重新登入後接回；已完成的一鍵更新不再被重新彈出成
  「正在恢復任務進度」。
- 背景任務狀態改用頂層 `running` 判斷，避免結果 JSON 裡的
  `runtime.running=true` 被誤判成任務仍在跑。
- 開機自動恢復限制在路由器啟動窗口，避免一鍵更新過程中的服務重啟誤觸
  第二次 takeover restore。
- 概覽與進階頁表格樣式微調，降低摘要 table 高度錯位與行背景干擾。

Release:

- [qoli/localclash-luci v0.1.0-31](https://github.com/qoli/localclash-luci/releases/tag/v0.1.0-31)
- [qoli/localclash-luci v0.1.0-32](https://github.com/qoli/localclash-luci/releases/tag/v0.1.0-32)
- [qoli/localclash-luci v0.1.0-33](https://github.com/qoli/localclash-luci/releases/tag/v0.1.0-33)
- [qoli/localclash-luci v0.1.0-34](https://github.com/qoli/localclash-luci/releases/tag/v0.1.0-34)
- [qoli/localclash-luci v0.1.0-35](https://github.com/qoli/localclash-luci/releases/tag/v0.1.0-35)
- [qoli/localclash-luci v0.1.0-36](https://github.com/qoli/localclash-luci/releases/tag/v0.1.0-36)

Release assets:

- `luci-app-localclash_0.1.0-36_all.ipk`
- `luci-app-localclash_0.1.0-36_all.ipk.sha256`
- `luci-app-localclash-0.1.0-r36.apk`
- `luci-app-localclash-0.1.0-r36.apk.sha256`

Verification:

- GitHub Release `v0.1.0-36` 已包含 OpenWrt 24.10 及更早版本使用的 IPK、
  OpenWrt 25.12 及更新版本使用的 APK，以及兩者 checksum sidecar。
- 真實路由器已完成一鍵更新到 `v0.1.0-36`，IPK 更新後任務能跑完；
  runtime running、router takeover effective。

## 2026-06-04

### localClash Core v0.1.30, v0.1.35-v0.1.38

Changes:

- 自 2026-06-02 Telegram 公告後，Core 已累積到 `v0.1.38`；本次整合
  `v0.1.30` 補發資產，以及 `v0.1.35` 到 `v0.1.38` 的正式更新。
- 訂閱輸入與多來源辨識補齊：支援 proxy URI 訂閱來源，節點名前綴優先使用
  `display_name`，多個訂閱來源更容易看懂。
- MCP 與下載更穩：工具調用不再接收 caller 傳入的 server-owned 路徑；
  amd64 Mihomo core 改用更保守的 `amd64-v1` 資產，降低老設備兼容風險。

Release:

- [qoli/localClash v0.1.30](https://github.com/qoli/localClash/releases/tag/v0.1.30)
- [qoli/localClash v0.1.35](https://github.com/qoli/localClash/releases/tag/v0.1.35)
- [qoli/localClash v0.1.36](https://github.com/qoli/localClash/releases/tag/v0.1.36)
- [qoli/localClash v0.1.37](https://github.com/qoli/localClash/releases/tag/v0.1.37)
- [qoli/localClash v0.1.38](https://github.com/qoli/localClash/releases/tag/v0.1.38)

Release assets:

- `localclash-linux-amd64`
- `localclash-linux-arm64`
- `localclash-base-assets.tar.gz`
- `localclash-release-manifest.json`
- 對應的 `.sha256` checksum 文件

Verification:

- GitHub Release `v0.1.38` 已包含 linux amd64/arm64、base assets、
  release manifest 和 checksum assets。

### localclash-luci v0.1.0-22-v0.1.0-30

Changes:

- LuCI 已進入可公開使用的小白流程：下載、安裝、訂閱、啟動都收斂在 LuCI
  頁面，不需要先理解 mihomo YAML、rules 或 proxy-groups。
- 概覽頁重做為摘要表格，會背景檢查 LuCI / Core 更新；進階頁保留 Core、
  Mihomo、Dashboard 等組件級維護。
- `一鍵更新` 串起 LuCI、localClash Core、Mihomo、Dashboard、訂閱刷新、
  配置重建、MCP 服務檢查與網路接管恢復。
- `v0.1.0-30` 起，訂閱刷新失敗時可明確使用既有 merged subscription cache
  繼續，但仍必須通過 config render 和 `mihomo config-test` 才會切換 runtime。

Release:

- [qoli/localclash-luci v0.1.0-22](https://github.com/qoli/localclash-luci/releases/tag/v0.1.0-22)
- [qoli/localclash-luci v0.1.0-23](https://github.com/qoli/localclash-luci/releases/tag/v0.1.0-23)
- [qoli/localclash-luci v0.1.0-24](https://github.com/qoli/localclash-luci/releases/tag/v0.1.0-24)
- [qoli/localclash-luci v0.1.0-25](https://github.com/qoli/localclash-luci/releases/tag/v0.1.0-25)
- [qoli/localclash-luci v0.1.0-26](https://github.com/qoli/localclash-luci/releases/tag/v0.1.0-26)
- [qoli/localclash-luci v0.1.0-27](https://github.com/qoli/localclash-luci/releases/tag/v0.1.0-27)
- [qoli/localclash-luci v0.1.0-28](https://github.com/qoli/localclash-luci/releases/tag/v0.1.0-28)
- [qoli/localclash-luci v0.1.0-29](https://github.com/qoli/localclash-luci/releases/tag/v0.1.0-29)
- [qoli/localclash-luci v0.1.0-30](https://github.com/qoli/localclash-luci/releases/tag/v0.1.0-30)

Release assets:

- `luci-app-localclash_0.1.0-30_all.ipk`
- `luci-app-localclash_0.1.0-30_all.ipk.sha256`
- `luci-app-localclash-0.1.0-r30.apk`
- `luci-app-localclash-0.1.0-r30.apk.sha256`

Verification:

- `v0.1.0-22` 到 `v0.1.0-30` release notes 持續記錄 LuCI JavaScript
  `node --check`、rpcd helper syntax check、IPK/APK build 與 checksum
  驗證。
- Docker OpenWrt 已安裝 `luci-app-localclash 0.1.0-30`，並透過 ubus
  驗證 `one_click_update`、`core_update_check` 和 `luci_update_check`。

## 維護規則

新增 release 時，按下面順序更新這份文件：

1. 更新「目前最新版本」表格。
2. 增加一個以本地日期為標題的段落。
3. 分別列出 Core 與 LuCI 的變更；沒有發佈的 channel 不需要新增條目。
4. 只寫使用者或維護者需要知道的變更，不逐字複製 commit log。
5. 若 release 影響安裝、更新、manifest、OpenWrt package 或路由器行為，
   補上驗證證據。

## Telegram 頻道通知

Telegram 更新通知由 `telegram/top.md` 的固定頭部，加上本文件的最新日期
區塊生成：

```bash
scripts/telegram-channel-update.py --dry-run
```

預設頻道與 Syncnext 相同，為 `@RonnieAppsChannel`。正式發送時：

```bash
scripts/telegram-channel-update.py
```

正式發送預設會附加本機更新圖：

```text
telegram/localclash-telegram-update.png
```

如需只發文字，可以使用：

```bash
scripts/telegram-channel-update.py --no-image
```

Bot token 讀取順序：

1. `TELEGRAM_BOT_TOKEN`
2. `telegram/.token`
3. `/Volumes/Data/Github/SyncnextProjects/Syncnext/telegram/.token`

固定公告頭部維護在 `telegram/top.md`，腳本會把最新日期區塊提取出的
固定公告頭部維護在 `telegram/top.md`，已公告版本游標維護在
`telegram/broadcast-state.json`。腳本只會提取游標之後的新 release blocks，
避免同一天內已公告過的舊 changelog 被重複發送。正式預覽或發送時，文字
內容由 `telegram/top.md` 加上提取出的 `telegram/changelog.md` 組成；帶圖
發送時若 caption 超過 Telegram 的 1024 字限制，腳本會直接失敗，要求先
縮短公告，不會自動拆成「圖片 + 獨立文字」。生成文件、本地 token 和發送
記錄目錄都在 `.gitignore` 中，不能進入 Git 追蹤。
