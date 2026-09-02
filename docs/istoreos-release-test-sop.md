# iStoreOS QEMU 發版前測試 SOP

狀態：現行驗收規範。制定日期：2026-09-03。

本文件是 localClash Core 與 localclash-luci **唯一的發版功能驗收 SOP**。
每次發布任一專案，都要鎖定配對版本，在真正的 iStoreOS QEMU 韌體中完成
任務鏈；不是只確認 CI 綠燈、安裝包可解壓、UI 正常或程序存活。
文件存在不表示某個版本已通過；每次執行必須另有綁定產物的驗收紀錄。

## 1. 範圍、責任與安全界線

- 唯一功能驗收環境：x86_64 iStoreOS QEMU。Docker OpenWrt、Docker 模擬
  opkg 安裝及 UTM 效能 VM 不再是本專案發版驗收入口。
- ARM64 實機不是本 SOP 的發版門檻。雙架構封裝／checksum 檢查仍保留，
  但不得把 x86_64 QEMU 結果宣稱為 ARM64 或實體路由器網路驗收。
- 單元、契約、語法及封裝檢查是前置條件，不替代 QEMU。IPK／APK 建置使用
  Docker 是建置工具，不是被退役的 Docker 測試環境。
- Core 負責訂閱、策略／配置材料交易、Mihomo 生命週期及版本化 runtime facts。
  LuCI 負責 UI、ACL、RPCD、套件／procd、fw4/nft、policy routing、DNS hijack、
  接管及跨層恢復交易。兩層都必須取證。
- 只操作本輪指定的可拋棄 VM。不得清理或替換生產路由器的配置、訂閱、binary、
  防火牆、DNS、服務或接管狀態；也不得為本測試修改宿主機的預設路由／DNS。
- 全新基線使用 overlay 重建，不沿用舊真機清理指南，也不使用廣域刪除或
  `pkill -f`。錯誤注入只作用於事先列明的 VM 內測試副本或測試資源。
- 訂閱 URL、token、密碼、WireGuard 私鑰、完整配置不得入版控或公開證據。
  原始證據置於受限本機目錄；分享前脫敏。不要任意 export 容器／整個 workdir。

## 2. 任務鏈與通過規則

```text
G00 候選版本與前置檢查
  → G01 韌體、管理通道、WAN/LAN 與輸入基線
  → G02 真實候選更新來源
  → G03 離線安裝及拒絕邊界
  → A / B / C / D / E（各自獨立基線，每條結尾執行 V）
  → F 一般功能 → N 實際網路 → R 重啟恢復 → X 失敗與重試
  → Z 重置再初始化 → G99 證據審核與發版判定
```

每項必填：ID、前置狀態、輸入、操作、預期、實測、證據、耗時、結果、恢復方式。
結果只有 `PASS`、`FAIL`、`BLOCKED`、`N/A`：

- 預期拒絕的負向案例，在明確拒絕且狀態保護正確時才是 PASS。
- 未執行、環境不具備、只有模擬結果或缺少證據，一律 BLOCKED，不是 PASS。
- A–E、V、IPv4 的 N、R、X、Z 不得因時間或既有故障而標 N/A。
  只有本文明列的條件分支（F8 最佳化成功、N5 IPv6、X7 不相容舊策略）可 N/A，
  且必須附不適用證據。
- 已知問題不自動豁免。必跑項 FAIL／BLOCKED 時，本次完整驗收不通過。
- 上一關失敗時停止其下游；可以在另一獨立基線採集其他案例，但不能沖銷失敗。
- 測試中修正程式或替換候選資產，產生新的候選識別並重跑受影響的情境、V、
  N 及相關 R/X；若變更初始化、更新或共用材料交易，重跑 A–E。
- 發布責任人審核 G99 前，不推送會觸發公開發布的 tag、不發布或廣播正式 Release。
  現有 CI **沒有自動執行／強制核驗 QEMU 報告**；這是人工放行關卡，不能稱為自動門禁。

## 3. G00：鎖定候選、舊版與前置檢查

1. 記錄 Core／LuCI 的 commit、目標版本、工作樹差異、source locks，以及
   Mihomo Meta／Smart、Dashboard、dnsqualify 和韌體版本。不得測試混合的未知 dirty build。
2. 保存候選 IPK、兩架構 `.run`、Core 資產、manifest 及 checksum 清單；記錄
   build/CI ID 與實際 SHA-256。依賴或下載內容改變，視為另一候選。
3. 在 Core 執行 `go test ./...`、`go vet ./...`、`git diff --check`。
   LuCI 執行現有 CI 的 JS／shell／Python／RPCD 檢查、
   `bash scripts/test-takeover-manager.sh`，以及資產驗證。
   建置指令見 LuCI release runbook；不在本 SOP 複製第二套發布流程。
4. 舊版至少固定「上一個正式版本」的 Core／LuCI 配對與原始安裝包。
   本次跨越配置格式、更新協定或 helper 交接變更時，再加入跨越該變更的關鍵舊版。
5. Core-only 發版配對目前受支援的 LuCI；LuCI-only 發版配對鎖定的 Core。
   兩者一起改時測精確新配對。未改動的元件仍須記錄，不自行假定 latest 相容。

**通過：**來源與資產可追溯，前置檢查通過，舊版能建立真實基線。
**失敗處理：**停在 G00；不得用手改版本字串、手動拷入 binary 偽造已安裝舊版。

## 4. G01：QEMU、網路與輸入基線

### 4.1 韌體與管理通道

現有入口：[scripts/istoreos-test-env.sh](../scripts/istoreos-test-env.sh)。
預設固定值如下；實測仍要從 guest 讀回，不能把腳本設定當成 guest 現況。

| 項目 | 預設值 |
| --- | --- |
| 韌體 | iStoreOS 24.10.8，build 2026073111，x86_64 |
| 映像 | `istoreos-24.10.8-2026073111-x86-64-squashfs-combined.img.gz` |
| SHA-256 | `2ce609e2625f9ba67723ec29b0b509baa300c6b74f528596490d950909e09a9c` |
| QEMU | TCG、2 vCPU、2048 MB RAM；Apple Silicon 上為 x86 模擬 |
| 資料 | `.runtime/istoreos-qemu/`，唯讀 raw base＋可拋棄 qcow2 overlay |
| LuCI／SSH | `http://127.0.0.1:18089/`；`root@127.0.0.1:12223` |
| MCP／controller | `http://127.0.0.1:18766/mcp`；`http://127.0.0.1:19091/` |
| Console／VNC | 腳本 `console`；`vnc://127.0.0.1:5902` |

在 Core repo 根目錄執行；這些命令會建立／啟動指定測試 VM：

```bash
scripts/istoreos-test-env.sh status
scripts/istoreos-test-env.sh prepare
scripts/istoreos-test-env.sh start
scripts/istoreos-test-env.sh wait
```

先以 console 核對 guest 身分、LAN、時間、空間及 root 登入方式。乾淨韌體可能
沒有 root 密碼；SSH 金鑰變更時，透過 console 核對指紋再更新該 VM 的紀錄，
不要直接關閉主機金鑰驗證。HTTP 200 只證明入口可達，不是初始化成功。

腳本轉發目標是 `192.168.101.1`，但舊重置紀錄曾出現 guest LAN
`192.168.100.1`。必須讀回 `ubus call system board`、`ip addr` 與路由；
只在測試 overlay 內對齊管理 LAN。不要為修正轉發而改生產路由器。

保存基線前正常停止 VM，確認 PID 及其 command 確實對應該 overlay。
只備份停止狀態的 overlay，連同 base hash、韌體、網路及輸入識別保存；
不可複製正在寫入的 qcow2 當成可靠快照。每次回復後重查 guest 狀態。
需要從零重建時，先封存本輪證據，再使用腳本 `stop`、`reset`、`start`、`wait`；
`reset` 會刪除指定環境的 writable overlay，不會保存其設定。
多 VM 必須分開 runtime directory 與所有 host ports，不能共用同一 overlay。

### 4.2 WAN 與 LAN 證據

- 預設第一張 NIC 是 QEMU user-mode NAT WAN。宿主機上游可能已有透明代理，
  必須記錄；這種出口不能宣稱為裸 WAN 或直接代表代理節點的真實 WAN 品質。
- 若使用既有 WireGuard WAN-equivalent 路徑，記錄 endpoint route、VM default
  route、近期 handshake、測試期間 transfer 增量、上游 bypass 與出口 IPv4 摘要。
  歷史設定是 `wg_istore_wan`、上游 `10.66.67.1/30`、VM `10.66.67.2/30`。
  這不是每次重置後自動存在的能力；缺失時不能直接修改生產上游補建。
- 必須準備獨立的 **QEMU LAN 客戶端**，位於受測 iStoreOS 的 LAN、預設閘道
  指向該 guest；停用客戶端的 HTTP/SOCKS proxy、VPN 與其他旁路。
  可使用同一隔離 QEMU 虛擬網段上的 client guest，不使用 Docker。
- 記錄 client IP／gateway／DNS、guest ingress interface、目的端與回應內容；
  核對 nft/policy-route 計數與 controller connection chain。
  宿主機 host-forward、guest 本機 curl、指定 HTTP proxy 都不是 LAN 接管證據。
- 現有腳本只有 WAN/LAN user-mode NIC 與管理 port-forward，**不會自動建立
  LAN client 或共享的 QEMU LAN**。操作人須先提供並記錄可重現的 client 拓撲、
  啟動方式與成功進入 guest LAN 的證據；做不到就 BLOCKED，不能略過 N。
- 固定直連與代理的 HTTP/TCP 及 UDP echo/DNS 測試端點，先在接管前驗證服務正常；
  記錄預期回應、出口、逾時與探測間隔。不能僅靠某網站打得開推斷分流。

### 4.3 測試資料與三種基線

| 基線 | 必要狀態 |
| --- | --- |
| S0 乾淨韌體 | 無 localClash 套件、訂閱、runtime、接管；只有必要管理／測試網路設定 |
| S1 已安裝未初始化 | 套件／bundle 自帶 binary 和資源可存在；沒有訂閱、生成配置、能力快照、使用者策略或接管 |
| S2 已初始化 | 由指定版本的正式 UI 流程生成，V/N 通過；保存精確版本及狀態指紋 |

準備完整多來源真實訂閱與單節點 URI，保存來源匿名 ID、下載內容摘要、候選數、
刷新時間；原始輸入只放受限目錄。實際合格節點數可能變動，不硬編碼歷史數字。
另準備固定的大訂閱、可控失敗來源、直連／代理測試域名、合法及非法自訂規則、
用於資料保留測試的使用者檔案。大訂閱不得為縮短測試而裁成小樣本。
不使用 localhost 失效 URL 冒充真實遠端訂閱。

## 5. G02：候選更新來源必須真的可用

初始化和一鍵更新必須走產品下載、checksum、安裝與服務交接流程，不以
mock Core／mock opkg、手動覆蓋 helper／binary、預先填好 capability 代替。

1. 在測試環境配置精確的候選來源，分別覆蓋 Core manifest、LuCI release metadata、
   dnsqualify manifest、Mihomo 與 Dashboard 資產；記錄實際 URL 的脫敏表示與 SHA。
2. 現有 helper 提供 `LOCALCLASH_RELEASE_MANIFEST`、`LOCALCLASH_LUCI_RELEASE_API`、
   `LOCALCLASH_DNSQUALIFY_RELEASE_MANIFEST`。它們只是入口，**不是完成的候選源工具**。
   必須驗證環境配置進入 LuCI 所呼叫的 rpcd helper，且更新後的 re-exec／服務仍指向
   同一候選；在 SSH shell 設變數不等於 UI 已使用它。
3. 不得放寬來源 allow-list 或 checksum 驗證來跑測試。dnsqualify 目前要求
   官方 GitHub tag asset URL，離線 bundle 的 Core pin 也要求可驗證的官方
   tag manifest。候選供應方式必須符合這些契約。
4. 如現有管線只能取得公開舊版，或無法在正式發布前供應本輪候選，記錄 G02
   BLOCKED，先處理候選分發能力；不得先發布正式版再補驗收。新增私有候選
   發布／鏡像或更改 CI 是另外的實作工作，不是本 SOP 已完成的能力。
5. 舊版 S1/S2 必須先以舊版配對及其精確來源建立；凍結後才切換到新候選來源。
   否則「舊版初始化」可能已經下載新版，失去跨版驗收意義。

**通過：**UI 發起的實際下載及安裝版本均等於 G00；更新前後來源設定不漂移。
Core-only 可用現有 LuCI 安裝路徑測新 Core，不把未包含新 Core 的既有 `.run`
宣稱為新 bundle；只有宣稱新 bundle 時才要求它攜帶該候選 Core。

## 6. G03：iStore 離線安裝與拒絕邊界

每個破壞性案例從獨立 S0/S1 副本開始，保留 console／管理 LAN：

1. 阻斷 **測試 VM 的 WAN**，保留管理入口；在 iStore 離線安裝 UI 上傳本輪
   x86_64 `.run`，執行真實安裝。Core-only 發版可使用本輪鎖定的既有 LuCI bundle，
   之後初始化必須從 G02 取得新 Core；不得把既有 bundle 稱為包含新 Core。
   記錄斷網證據與安裝日誌。
2. 核對真實 `opkg list-installed luci-app-localclash`、套件檔案、Core／dnsqualify
   版本與 hash、policy/rule/geodata 基礎資源、LuCI menu／ACL／RPCD。
   安裝後必須是 S1，不能自行寫入訂閱、啟動 Mihomo 或接管。
3. 再次安裝同一包；額外的使用者檔案保持不變，沒有遺留的半套 `.new` 檔案。
4. 對照副本中安裝 aarch64 `.run`，必須拒絕架構不符，安裝前後正式檔案摘要不變。
   這是拒絕測試，不是 ARM64 成功安裝測試。
5. 解包候選的測試副本，改動 `bundle.env` 的非秘密版本欄位但不更新內部 checksum，
   執行該副本 installer；必須明確 checksum 失敗，正式檔案摘要不變。
6. 恢復 VM WAN 並驗證。後續初始化是可聯網流程；不可用離線安裝成功代替初始化。

若套件依賴導致離線安裝失敗，保留失敗並修正 bundle／支援條件，不臨時開網安裝
後改記成功。安裝中斷案例在 X 中處理，不假定整個 installer 具有全域原子回滾。

## 7. A–E：五條必跑路徑

完整預設策略為主線；不可用 minimal 或取消預設策略同步替代。A、B、E 分別
在 Meta、Smart 獨立基線執行；C、D 至少以完整預設配置跑一遍，核心選項另由 F 覆蓋。
每條都由真正 LuCI 操作開始，CLI／ubus 用於取證，不替代使用者入口。

| ID | 前置與操作順序 | 必須通過的結果 |
| --- | --- | --- |
| A | S0 → G03 候選安裝 → S1 → 填入完整訂閱 →「开始初始化」→ V/N | 無殘留依賴；元件、訂閱、能力、策略、配置、runtime、接管全鏈完成 |
| B | 本輪候選 S2 →「一键更新」→ V/N → 再次「一键更新」→ V/N | 同版本可重跑、不意外降級、不重複啟動、不留任務鎖；不是只測版本檢查按鈕 |
| C | 舊版 S1 → 使用受支援的套件升級入口裝候選 LuCI → 讀回版本 → 新版 UI 初始化 → V/N | 升級不假造配置；舊套件狀態下新版能完成全新初始化 |
| D | 舊版 S2＋資料保留樣本 → 套件升級候選 → 新版 UI 重新初始化 → V/N | 既有配置遷移正確；策略重建與資料保留符合第 10 節，不被舊 capability 卡住 |
| E | 舊版 S2＋資料保留樣本 → 從舊版 UI「一键更新」取得候選 → V/N | 真正跨版 helper 交接、Core/MCP 更新、兩個檢查點、使用者資料與網路均正確 |

C/D 記錄使用 `.run` 或 LuCI 套件更新入口，不混寫為 E。E 不得預先覆蓋新版 helper。
若關鍵舊版沒有交接機制，按該版支援的路徑先「检查 LuCI 更新」並完成獨立更新，
刷新頁面後再一鍵更新；報告標記「兩步升級」，不能宣稱直接一鍵升級。

另外從 S1 執行一次一鍵更新：元件可更新，沒有訂閱的材料階段應明確跳過，
不得宣稱完成初始化或自行啟動接管；接著輸入訂閱仍能完成 A 的初始化後半段。
從「已配置但使用者已停止 runtime／接管」的 S2 副本更新，也不得擅自重新開啟。

## 8. V：每條路徑結尾的共同成功判定

必須逐層讀回，不以單一 `ok:true`、退出碼或 UI 綠色狀態取代：

| 層 | 必需證據 |
| --- | --- |
| V1 UI／任務 | 起始操作、task ID/PID、階段日誌、terminal result、結束時間；不再 running、鎖釋放，頁面與後端一致 |
| V2 元件／服務 | 精確 Core/LuCI/Mihomo/dnsqualify/Dashboard 身分；procd MCP instance 與 HTTP health 均正常 |
| V3 材料 | 本輪真實來源摘要、節點／候選／合格數、策略／自訂網站摘要、配置 SHA、材料提交結果；無舊快照冒充新結果 |
| V4 配置／runtime | `mihomo -t`／attestation 成功；managed PID、flavor、router profile、controller、載入代理組與配置相符 |
| V5 接管 | LuCI `takeover_status` effective、ownership、TUN、policy rule/route、nft/DNS 檢查與本輪配置一致 |
| V6 網路 | 本情境下重新執行 N1–N4，附 LAN 客戶端與實際直連／代理出口證據 |

在 **已驗證身分的測試 guest** 內可使用以下唯讀入口：

```sh
ubus call system board
opkg list-installed luci-app-localclash
ubus call localclash status
ubus call localclash task_status
ubus call localclash takeover_status
ubus call localclash boot_restore_status
ubus call localclash custom_sites_get
ip rule show
ip route show table 27747
```

配合已安裝 Core 的 `version`、`runtime status --json`、controller `/version`、
`/proxies`、`/rules`、`/connections` 和當輪配置 hash。controller 認證從 VM 安全取得，
不要把 secret 放到命令紀錄或截圖。Mihomo 配置驗證使用產品隔離驗證路徑，
不要手動對正在運行的 Smart workdir 啟動另一個 `mihomo -t` 去爭用 cache.db。

熱載入可能保持 PID 不變；process restart 才檢查替換 PID。RPC 超時是回應失敗，
不等於程序沒重啟；繼續唯讀觀察終態，但仍記錄 UI／RPC 問題，不偷偷重按操作。

### 一鍵更新的兩個檢查點

- **軟體檢查點**：候選 Meta/Smart 準備與驗證後替換；更新前仍運行的服務以現行
  配置完成 process restart，MCP、controller 與接管回到可驗證狀態。
- **材料檢查點**：訂閱、選定的預設策略、能力與配置在交易中生成／驗證，熱載入
  並讀回；必須核對 `checkpoints.software` 和 `checkpoints.material` 的具體狀態。
- 未初始化／原本停止的分支允許明確 skipped 或未 activated，但不得把它當完整 S2。
- 第二階段失敗應保留最後已成功提交的檢查點，不要求把第一階段已成功的新軟體
  一律降回舊版。回滾驗證比較該階段開始前的材料摘要與真實網路。

## 9. F：一般功能性可用範圍

每項由可工作的 S2 副本開始；改動後檢查 task、材料、runtime 讀回與對應真實請求，
結束後恢復基線，避免一項的設定影響下一項。

| ID | 操作 | 通過條件 |
| --- | --- | --- |
| F1 訂閱 CRUD | 加入兩個以上來源及單節點 URI、修改、刪除、保存並應用、刷新；另測空白／非法輸入 | 合併／移除與來源歸屬正確；錯誤不清空合法狀態；重新開頁仍有保存內容 |
| F2 大訂閱 | 固定完整大樣本執行保存／刷新，覆蓋過去 240 秒邊界 | heartbeat 持續、無固定短外層 deadline 誤殺；完成後 V 通過；不能靠無限延長無進度任務過關 |
| F3 策略／核心 | 完整預設 Meta、完整預設 Smart、minimal 選項各自初始化／重新初始化 | 選用核心及生成／已載入結構符合選擇；minimal 只驗選項，不替代主線 |
| F4 网站分流 | 新增／刪除直連與代理域名、子域、萬用字元；同域兩邊與先後新增順序；非法 pattern | 最新成功規則優先、警告與刪除後恢復較舊規則正確；實際命中／出口符合，不只列表更新 |
| F5 runtime／接管 | 啟動、重啟、停止；runtime 保持運行時單獨停止接管，再套用接管 | UI 與後端分開表達兩種狀態；只移除 localClash-owned 規則；無重複規則或 orphan 程序 |
| F6 Dashboard | 從 LuCI 開面板，讀版本、代理組、連線，選擇專用測試組的出口並發請求 | 認證／資源／API 正常，選擇確實生效；不因面板載入成功就算路由通過 |
| F7 MCP | 按頁面接入資訊連 QEMU MCP，完成協定 initialize、tools/list、environment_inspect 及路由唯讀查詢 | 連到正確 guest、工具可呼叫、結果與 LuCI／controller 相符；不是只 GET health |
| F8 DNS 最佳化 | 查看基線，執行 dnsqualify；有合格環境時套用並明確重啟，再刪除設定；測證據過期／WAN 變更拒絕 | 一般解析與 LAN 名稱正常；可用／不適用／失敗如實顯示；刪除回到加密 DNS 基線，不暗改節點 DNS |
| F9 元件維護 | 分別操作 LuCI、Core、Mihomo、Dashboard 的可見維護入口與 MCP 服務停止／啟動 | 實際版本及服務符合結果；之後仍可初始化／更新與連接 MCP，沒有半更新狀態 |
| F10 UI／任務 | 各頁與狀態區、長任務日誌、重複點擊、重新載入頁面再觀察原任務 | 不並行啟動互斥寫入；不因斷開頁面遺失終態；沒有未處理錯誤或無限 busy |

F8 的成功最佳化子案例，僅在已證明 WAN 不符合產品資格時可 N/A；資格判定、
明確拒絕、一般 DNS、刪除與基線恢復仍必測。不能製造假 public IP／假資格當成功。

## 10. 更新／重新初始化的資料保留矩陣

從相同 S2 克隆兩份，一份開啟預設策略同步，另一份關閉。分別執行 B/E；
策略同步開啟仍是主線，不因自訂策略會被覆蓋就關掉它。D 另外核對 UI 的重置承諾。

| 資料 | 預設策略同步開啟 | 預設策略同步關閉 |
| --- | --- | --- |
| 訂閱來源 | 保留並真正刷新，不用舊快照冒充成功 | 相同 |
| 使用者自訂策略補丁 | 按明示確認被最新預設策略重建／覆蓋 | 保留；若舊契約不相容，明確失敗而非偷換策略 |
| 「网站分流」代理／直連列表 | 保留數量、內容摘要與順序；與自訂策略補丁是不同資料 | 相同 |
| 同步策略偏好 | 保存選擇；重新開頁與下一次更新一致 | 相同 |
| 開機恢復意圖 | 不因更新自行啟用／停用 | 相同 |
| 使用者額外資源檔 | 安裝包允諾保留的額外檔案不丟失 | 相同 |

目前自動選擇組使用完整可選節點，ChatGPT capability 獨立產生。新配置不得再依賴
已移除的 g204 capability；舊 intent 在不更新策略時明確拒絕可作負向 PASS，
但必須接著走同步新版預設策略的支援路徑成功，不能把拒絕本身當完成升級。

## 11. N：真正的網路功能

G01 的 LAN client 與固定端點是前置條件。每輪記錄實際目的地、時間、回應及
本輪 policy/controller/nft 證據；事先在紀錄中固定探測逾時和允許的切換中斷預算，
不得看到結果後才放寬標準。每種正常狀態至少連續探測三次，保留全部失敗。

| ID | 測試 | 必需結果 |
| --- | --- | --- |
| N1 直連 | LAN client → 指定直連域名／HTTP 端點 | 正確回應及直連 chain／出口，不能其實被上游代理接走 |
| N2 代理 | 同一 client → 指定代理域名／HTTP 端點 | 命中測試規則和預期代理鏈／出口；與直連對照，不只看 HTTP 200 |
| N3 DNS／區網 | 公開域名、router DNS、固定本地域名／DHCP 名稱、LAN 服務 | 正確答案及連通性；公共 DNS 接管沒有破壞本地解析／連線 |
| N4 UDP | client 對固定 UDP echo 或受控 UDP DNS 端點發唯一測試 payload | 有應用層回應、TUN／路由計數及預期出口；TCP 成功不能替代 |
| N5 IPv6 | 已宣稱支援且具可驗證 IPv6 路徑時重跑 N1–N4 | 正確 IPv6 捕獲／出口／DNS；未配置 IPv6 可 N/A，但報告不可宣稱驗證 IPv6 |
| N6 連續性 | 初始化後、更新各檢查點、重啟／停止／恢復期間持續探測 | 準備階段不提前破壞舊鏈；切換中斷與恢復時間可量測，無切換後持續黑洞 |

明確停止期間不要求代理路徑通過；要驗證已停止及正常直連／管理通道。
只有執行恢復後，才按預先設定的恢復預算要求代理及接管重新可用。

若環境無法區分 VM 代理與上游透明代理，該出口結論 BLOCKED；不能拿相同 public IP
作為唯一證據。指定 proxy curl 可作輔助診斷，不取代 LAN-forwarded N1–N4。

## 12. R：真正重啟與恢復

每個案例從獨立 S2 執行，操作前後保存 boot ID、PID、意圖、接管與 N 證據。

| ID | 操作 | 通過條件 |
| --- | --- | --- |
| R1 開機恢復開 | UI 開啟後在 guest 執行真正 reboot | boot ID 改變；MCP、runtime、接管依意圖恢復，N 通過 |
| R2 開機恢復關 | 關閉後 reboot | boot ID 改變；不因舊 repair ticket 自動接管；明確手動啟動後 N 通過 |
| R3 WAN 事件 | 在 guest 觸發已記錄 WAN 的 ifdown/ifup／ifupdate | 原本已接管時依 same-boot 意圖恢復；不改宿主或生產 WAN |
| R4 明確停止 | 停止接管，再觸發 WAN 事件與等待背景恢復窗口 | 接管保持停止，不被過時 worker 重新開啟；手動恢復後 N 通過 |
| R5 非預期退出 | 核對 managed PID 後，分別在空閒與更新期間對該測試程序注入退出 | 觀察 watchdog、任務與 LuCI 恢復交易；不得只因新 PID 就報健康；接管未恢復必須明確顯示並可按支援流程恢復 |
| R6 重複生命週期 | 連續三輪開始／重啟／停止接管與 runtime，再恢復 | 運行／重啟後 V/N 通過；停止後無 owned 接管殘留、runtime 確實停止且不被擅自重啟；最後恢復再跑 V/N，無規則、路由、PID、鎖或暫存資產累積 |

R5 中如果產品承諾自動恢復，必須自動完成；未承諾的邊界須有明確可操作失敗狀態，
不能將人工修復寫成自動恢復通過。歷史 watchdog 接管缺口不是免測理由。
單獨 firewall reload 若仍需手動套用，按當版明示契約驗證；不得描述成已自動修復。

## 13. X：失敗注入、取消與重試

每次只注入一個故障。先列明目標、預期錯誤、前後檔案摘要、允許保留的檢查點、
恢復動作與退出條件；由獨立 overlay 保護其他案例。產品 timeout 不得為過關而修改。
對大訂閱記錄 heartbeat、處理數與最後進度；無進度達本輪預定觀察預算時取證並
取消，標 FAIL／BLOCKED，不無限等待，也不把操作者取消當作產品原生 timeout。

| ID | 注入方式與時機 | 預期與重試 |
| --- | --- | --- |
| X1 下載／校驗 | 測試源暫時失聯、空檔或錯誤 checksum；先測可恢復短故障，再測持續故障 | 在既有次數／逾時內重試或明確失敗；不安裝未驗證檔、不把空輸出當成功；恢復來源後正常更新 |
| X2 訂閱失敗 | 在軟體檢查點完成後，讓受控訂閱回錯誤／無效內容 | 材料階段明確失敗；舊材料及已提交軟體檢查點可驗證，N 不持續中斷；修正來源後整條更新成功 |
| X3 配置／熱載入 | 在測試副本用可控非法材料觸發驗證拒絕；另測驗證後 controller 暫時不可達 | 明確拒絕／失敗，無半套新材料或假成功；核對前後摘要與實際已載入狀態，再恢復並重試 |
| X4 安裝／啟動失敗 | 在專用副本中以受控資源不足或占用目標 listener 觸發失敗 | 定位到安裝／啟動階段；保留可診斷結果，不盲目反覆啟動；移除注入後支援的修復路徑成功 |
| X5 取消 | 初始化與一鍵更新的準備、材料階段分別點取消 | 子程序及任務有終態、鎖釋放；無延遲 worker 又覆蓋狀態；保留合法檢查點，再按正常入口成功 |
| X6 RPC/UI 中斷 | 更新中重新載入頁面／中斷客戶端連線，另觀察套件 re-exec | 可找回同一任務終態，不重複執行；新 helper 與 MCP 身分正確，V/N 通過 |
| X7 不相容舊策略 | 關鍵舊版配置關閉預設策略同步後更新 | 明確拒絕已移除契約，不暗自替代；資料保持可恢復，開啟同步後真正升級成功 |

X7 只在 G00 的支援／遷移範圍包含已移除或不相容契約時執行，並固定能重現該
契約的歷史基線。若本次舊版配置全部相容，附契約差異證據後可標 N/A；不得為了
滿足「應拒絕」而把正常相容升級判失敗。一般非法輸入／驗證拒絕仍由 F1、F4、X3 必測。

任何恢復先保存故障證據。管理頁不可達時使用已確認的 console；不要改宿主網路，
不要照搬生產路由器清理指令。無法安全恢復時停止 guest，保留失敗 overlay，
從乾淨副本再測；失敗紀錄仍保留在本次結果中。

## 14. Z：重置與重新開始

從含資料保留樣本的 S2 副本開始，先用產品操作停止接管與 runtime，確認停止成功。
分別驗證 UI 提供的配置重置和完整 workspace 重置；記錄確認文字與實際刪除計畫。

- 配置重置清除訂閱／生成配置／runtime 狀態等宣告範圍，保留套件、下載程式及
  約定保留的 `localclash-user.json`。不得自行擴大範圍。
- 完整 workspace 重置核對 workspace marker 與固定範圍；不刪 LuCI 套件或
  workspace 外的 Core。不能接受任意路徑或刪除其他應用的狀態。
- 重置後 UI 顯示真實未初始化狀態，沒有殘留接管；重新走「开始初始化」及 V/N。
- 重置功能本身不能替代 G01 的乾淨 S0 證明；A 仍必須由乾淨韌體開始。

## 15. G99：證據模板與發版放行

每輪在 `.runtime/istoreos-acceptance/<run-id>/` 保存一份執行紀錄及脫敏證據。
`run-id` 包含日期和候選識別；不要把歷史目錄中的 success.json 當作本輪結果。
以下是紀錄模板，不是另一份 SOP：

```yaml
run_id: <date-and-candidate-id>
sop_revision: <core-commit-containing-this-sop>
operator: <reviewer>
candidate:
  core: {commit: <sha>, version: <version>, artifact_sha256: <sha256>}
  luci: {commit: <sha>, version: <version>, artifact_sha256: <sha256>}
  dependencies_manifest: <restricted-evidence-path>
  update_source_evidence: <redacted-path>
environment:
  firmware_sha256: <sha256>
  overlay_baselines: <S0-S1-S2-identities>
  wan_and_lan_topology: <redacted-path>
inputs:
  old_version_pairs: <exact-pairs>
  subscriptions_digest: <digest-not-urls>
  endpoint_expectations_and_timeouts: <path>
cases:
  - id: <A-meta-or-X2>
    before: <baseline-and-hashes>
    operation: <UI-operation-and-task-id>
    expected: <state-and-network>
    actual: <observed-result>
    evidence: [<logs>, <hashes>, <runtime>, <takeover>, <requests>]
    duration_seconds: <number>
    result: <PASS-FAIL-BLOCKED-N/A>
    reason: <required-for-non-PASS>
    recovery_and_retest: <result-or-not-needed>
release_decision: <PASS-or-BLOCKED>
reviewed_by: <reviewer>
```

放行者逐項確認：

1. G00–G03、A–E（含指定核心／舊版分支）、V、F、N、R、X、Z 都有結果，
   所有必跑項 PASS；N/A 只有允許的條件分支，沒有未處理的 FAIL／BLOCKED。
2. 訂閱／配置內容、task、PID、controller、接管與 LAN 請求證據屬於同輪候選；
   不以 CI、封裝結果、舊事故紀錄或後來的手動修復頂替。
3. 大訂閱沒有縮小、預設策略同步未被主線關閉、失敗請求沒有被成功重試抹掉。
4. 發布 tag 必須指向受測 commit。若 Release workflow 重建資產，下載後對照
   已驗收的 hash；不一致不得沿用通過紀錄或繼續廣播，須調查／重驗。
   要求「公開前逐位元相同」時，管線必須先具備候選資產直接提升的能力；
   現行 tag 自動發布不能被文件描述成已具備該能力。
5. 證據脫敏，記錄未覆蓋平台／協定，明確寫「iStoreOS QEMU 驗收」，不擴大成實機結論。

## 16. 文件與工具的唯一入口

- 本文件擁有環境、測試任務、證據及放行規則；不另維護 Docker／真機測試 SOP。
- [QEMU 腳本](../scripts/istoreos-test-env.sh)只管理 VM，不是自動全測 runner。
- [Release Manifest](release-manifest.md)與
  [LuCI release runbook](https://github.com/qoli/localclash-luci/blob/main/docs/github-release-runbook.md)
  保留構建／發布流程，引用本 SOP 作功能驗收門檻。
- [Router Incident Register](router-incident-register.md)及 changelog 保留歷史證據，
  不作當版驗收結論。已退役方案中的舊環境／架構要求不覆蓋本文件。
- G01 LAN client、G02 候選供應、CI 報告強制校驗、候選資產提升都必須如實記錄
  已具備與缺失部分；本次文件整理不代表已實作這些配套工具或已跑完任何版本驗收。
