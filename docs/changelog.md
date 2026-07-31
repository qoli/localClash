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

## 目前最新版本

| 渠道 | 最新版本 | 發佈時間 |
| --- | --- | --- |
| localClash Core | [v0.1.48](https://github.com/qoli/localClash/releases/tag/v0.1.48) | 2026-07-31 UTC+8 |
| localclash-luci | [v0.1.0-42](https://github.com/qoli/localclash-luci/releases/tag/v0.1.0-42) | 2026-08-01 UTC+8 |

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
