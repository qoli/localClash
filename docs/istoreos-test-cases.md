# iStoreOS 功能案例參考

狀態：現行操作與斷言參考；2026-09-08 從舊 SOP 技術章節整理。

先讀 [SOP](istoreos-release-test-sop.md) 與 [功能追蹤表](istoreos-test-features.md)。
本文件解釋選定功能如何驗證，不提供每輪必跑清單，也不擁有放行結論。
保留 G00–G03、A–E、K/F/N/R/X/Z 編號供歷史證據對照；編號與章節順序不是執行依賴。
功能表 ID 是追蹤入口；本輪計畫明列所選子斷言、核心、基線、版本與證據要求。
案例中的「必須／通過」只約束已選案例，不把其他參考案例自動納入本輪。
V 是相關功能的分層取證清單，不是另一套全部重測的產品功能表。
只有需要該能力的案例才準備對應 VM、候選安裝來源、歷史基線或受控端點。
狀態用語以新版 SOP 為準；保留的歷史報告 PASS／FAIL／BLOCKED 不改寫。

## 3. G00：候選與選測前置資料

### 3.1 受測目標、歷史起點與實際身分

- 預設從待發布專案在本輪開始時的 HEAD 建置未發布候選，鎖定 commit 與產物
  SHA；若使用者指定其他 ref，須明列，不再稱最新 HEAD。Core-only／LuCI-only
  的未發版元件按下列配對規則固定；兩者一起驗收時分別鎖定兩個 HEAD。
- 版本號只是安裝／更新協定的標籤，不是 source commit 證明。不允許以公開
  latest Release 代替候選，也不要求先正式發布才能測試。測試期間不追逐漂移的
  latest；修復後鎖定新 commit、重建新候選並按SOP 的影響選測規則重驗。
- 每台 VM／每次操作先記用途（候選驗收、歷史基線建立、補救診斷）、端口及
  預期版本。進度回報與錯誤報告必須同時寫出用途與**故障當下實際版本**；
  歷史副本不能因任務名稱有 HEAD 就被當成最新候選。
- 分別核對「安裝包內版本」「本輪更新來源目標」「安裝後磁碟檔案」「實際執行
  程序／helper」。Core binary、已載入的 MCP executable、LuCI RPCD helper／
  接管腳本均須與該階段鎖定的 owning repo 檔案相符：歷史階段核對歷史版本，
  候選階段核對候選；不能只驗 Core 而漏掉舊 LuCI。
  Mihomo Meta／Smart 及模型則使用本候選鎖定的依賴，不自動改測上游 HEAD。
- A/C 的首次初始化由候選 LuCI 入口執行。若正式初始化包含先更新 Core，
  明列包內舊 Core → 候選 Core 的交接並核對身分；取得候選前的舊 Core 結果
  不可算候選功能證據。包內仍是舊 Core，就不得宣稱驗過「內建 HEAD Core 的包」。
  若本輪要求驗證內建 HEAD 的新 bundle 而無法提供，該安裝案例記 NOT_RUN 並列候選供應缺口。
- 歷史正式版本只在 C/D/E 的明示起點使用；其初始化失敗也按[SOP 的結果與異常規則](istoreos-release-test-sop.md#results)登錄。
  已含修復的新候選必須另從合格基線重驗；不得拿舊版 FAIL 直接宣稱新 HEAD
  仍有同一 Bug，也不能只因 commit 存在就宣稱已通過。歷史缺陷保留追溯，
  新候選的放行看其實測結果及必需升級基線是否合格，不永久繼承舊版 FAIL。

### 3.2 候選與前置檢查清單

1. 記錄 Core／LuCI 的 commit、目標版本、工作樹差異、source locks，以及
   Mihomo Meta／Smart、Dashboard、dnsqualify 和韌體版本。不得測試混合的未知 dirty build。
2. 保存候選 IPK、兩架構 `.run`、Core 資產、manifest 及 checksum 清單；記錄
   build/CI ID 與實際 SHA-256。依賴或下載內容改變，視為另一候選。
3. 依 owning repo 的要求執行前置檢查；Core 的既有入口為
   `go test ./...`、`go vet ./...`、`git diff --check`。
   LuCI 執行現有 CI 的 JS／shell／Python／RPCD 檢查、
   `bash scripts/test-takeover-manager.sh`，以及資產驗證。
   建置指令見 LuCI release runbook；不在本 SOP 複製第二套發布流程。
4. 涉及安裝／遷移功能時，固定所選舊版的 Core／LuCI 配對及原始安裝包。
   跨越配置格式、更新協定或 helper 交接變更時，選擇能覆蓋該契約的代表性舊版。
5. Core-only 發版配對目前受支援的 LuCI；LuCI-only 發版配對鎖定的 Core。
   兩者一起改時測精確新配對。未改動的元件仍須記錄，不自行假定 latest 相容。
6. Meta 與 Smart 分別記錄下載來源／解析後 URL、release/build 字串、來源 commit
   （若資產提供）、架構、binary SHA-256，及舊／新版本配對；不可假定二者來自
   相同上游或版本編號。Smart 額外記錄模型來源、SHA、更新設定和資料格式資訊。
   模型自動更新按產品契約運作；每個 attempt 記錄前後模型 SHA 與事件。
   模型改變只使依賴模型行為的證據需重新判斷，依計畫補驗相關項；
   不以同一模型的結果混算，也不作廢與模型無關的功能。

**就緒：**所選功能需要的來源、資產及前置可追溯；不要求無關舊版基線先全部完成。
**失敗處理：**候選身分／產物／前置檢查不合格時停在 G00。若只在歷史基線建立
失敗，登錄該缺陷、阻擋依賴它的升級分支；仍可在獨立合格副本驗候選 A 等功能。
不得用手改版本字串、手動拷入 binary 偽造已安裝舊版，或用 workaround 隱藏缺陷。

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
  啟動方式與成功進入 guest LAN 的證據；無法就緒時記環境 ERROR，依賴該拓撲的案例保持 NOT_RUN；不宣稱 N 通過。
- 必要功能使用受控 WAN 側 HTTP/TCP、UDP echo／DNS 回應端及可識別出口的
  測試代理；預先驗證端點健康、固定預期 payload／答案、路由意圖、逾時與探測
  間隔。請求須由 LAN client 經受測 router／Mihomo，不可用純 LAN bypass
  或宿主 mock 成功取代。受控環境可用隔離 QEMU 拓撲，不要求裸 ISP WAN。
- 公共網站、公共 DNS、真實訂閱節點的可達性／延遲另列外部觀察，不能拿
  「三次皆成功」當成必要功能的唯一斷言。記錄其地區、協定、預期直連／代理
  及選用理由，不能假設所有裸 IP 都應 DIRECT 或所有海外服務都可直接到達。
- 受控端點與可識別測試代理是需具備的測試資源，不宣稱現有 VM 腳本已提供。
  缺少所選案例需要的資源時，列出具體缺失的功能證據，不要求修改 localClash 來修復外網品質。

### 4.3 測試資料與三種基線

| 基線 | 必要狀態 |
| --- | --- |
| S0 乾淨韌體 | 無 localClash 套件、訂閱、runtime、接管；只有必要管理／測試網路設定 |
| S1 已安裝未初始化 | 套件／bundle 自帶 binary 和資源可存在；沒有訂閱、生成配置、能力快照、使用者策略或接管 |
| S2 已初始化 | 由指定版本的正式 UI 流程生成，V/N 必要功能通過；保存精確版本及狀態指紋，外部品質觀察另列 |

基線紀錄來源、身分及已驗證能力；建立失敗只阻擋真正依賴它的案例。
可使用另一份有來源與成功證據的既存快照。正常初始化基線與已知故障／正式恢復
後的遷移基線分開標示；後者能測候選恢復／遷移，但不能冒充舊版正常初始化。
缺少舊版正常 S2 時，先核對既存快照及候選支援的遷移契約，不要求修好舊版才能測新版本。

S1 為各核心的獨立副本；S2 必須區分 `S2-meta`、`S2-smart` 及舊／新版。
Smart 再區分未預置模型／統計的冷啟動基線，以及已成功載入模型、有受控流量與
已持久化統計的暖啟動基線。Meta 也分首次啟動與已有一般快取的再次啟動。
不得把 Meta 的 S2 改個名稱當作 Smart 基線。現行產品以各自獨立初始化的
S2-meta／S2-smart 驗證兩種核心，不以手換 binary 模擬不存在的運行中切換。
Smart 冷啟動不得從其他 VM 或已運行的 runtime 預先複製 `Model.bin`、統計或 cache。

依所選訂閱／初始化案例準備完整多來源真實訂閱與單節點 URI，保存來源匿名 ID、下載內容摘要、候選數、
刷新時間；原始輸入只放受限目錄。實際合格節點數可能變動，不硬編碼歷史數字。
按所選案例另準備固定的大訂閱、可控失敗來源、直連／代理測試域名、合法及非法自訂規則、
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
4. 如現有管線只能取得公開舊版，或無法在正式發布前供應本輪候選，記錄候選供應 ERROR 及依賴案例 NOT_RUN，
   先處理候選分發能力；不得先發布正式版再補驗收。新增私有候選
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

## 7. A–E：安裝、初始化與更新路徑

本節是功能適用範圍與操作參考。每輪只執行選測計畫列出的路徑／斷言；
未受影響且有適用證據的功能沿用原結果，不因發版而全部重跑。

完整預設策略是各路徑的正常主線；minimal 只用於其專屬分支。
Meta、Smart 各自保留證據，不能以一個核心的結果冒充另一個。選測計畫依
受影響核心、路徑與版本配對展開；共用 runtime／材料契約改動才補相關雙核心回歸。
每條都由真正 LuCI 操作開始，CLI／ubus 用於取證，不替代使用者入口。

| ID | 前置與操作順序 | 必須通過的結果 |
| --- | --- | --- |
| A | S0 → G03 候選安裝 → S1 → 填入完整訂閱 →「开始初始化」→ V/N | 無殘留依賴；元件、訂閱、能力、策略、配置、runtime、接管全鏈完成 |
| B | 本輪候選 S2 →「一键更新」→ V/N → 再次「一键更新」→ V/N | 同版本可重跑、不意外降級、不重複啟動、不留任務鎖；不是只測版本檢查按鈕 |
| C | 舊版 S1 → 使用受支援的套件升級入口裝候選 LuCI → 讀回版本 → 新版 UI 初始化 → V/N | 升級不假造配置；舊套件狀態下新版能完成全新初始化 |
| D | 舊版 S2＋資料保留樣本 → 套件升級候選 LuCI → 重新載入新版 UI → 從新版 UI 執行「一鍵更新」→ V/N | 候選 helper 以既有配置完成 Core／材料更新；配置遷移、策略重建與資料保留符合第 10 節，不要求不存在的 S2 重新初始化入口 |
| E | 舊版 S2＋資料保留樣本 → 從舊版 UI「一键更新」取得候選 → V/N | 真正跨版 helper 交接、Core/MCP 更新、兩個檢查點、使用者資料與網路均正確 |

A/C 的 Meta、Smart 分支分別選擇目標核心；D/E 主線保持原核心種類，驗證
`old-meta → new-meta`、`old-smart → new-smart`。現行產品沒有健康 S2 的
運行中核心切換入口，不以跨核心操作取代這兩條同核心升級證據。
選定的遷移功能綁定具體舊版配對及 C/D/E 路徑；不同配對不能拼成同一結果。
上一正式版是預設遷移參考，不因版本號遞增就新增三條全套回歸。

C/D 記錄使用 `.run` 或 LuCI 套件更新入口；D 在候選 LuCI 已安裝後由新版 UI
啟動一鍵更新。E 則從舊版 UI 啟動並驗證新版 helper 交接，不得預先覆蓋新版 helper。
若關鍵舊版沒有交接機制，按該版支援的路徑先「检查 LuCI 更新」並完成獨立更新，
刷新頁面後再一鍵更新；報告標記「兩步升級」，不能宣稱直接一鍵升級。

另外從 S1 執行一次一鍵更新：元件可更新，沒有訂閱的材料階段應明確跳過，
不得宣稱完成初始化或自行啟動接管；接著輸入訂閱仍能完成 A 的初始化後半段。
從「已配置但使用者已停止 runtime／接管」的 S2 副本更新，也不得擅自重新開啟。
未初始化／已停止更新是可獨立選測的分支，依計畫記錄核心及狀態。

### 核心適用範圍

| 案例參考 | 功能適用範圍（由計畫選定） | 證據限制 |
| --- | --- | --- |
| G00–G02 | 記錄兩種核心、模型與各自基線／更新來源 | 韌體、管理 LAN 等相同設定可 shared；核心資產與基線不可合併 |
| G03 | 安裝包完整性、真實離線安裝及拒絕邊界 | 尚未選用／啟動核心，可 shared；不代表任一核心已可運行 |
| A–E、V | 各路徑按核心、舊版配對選測 | 活躍核心與實际流程證據不可冒用 |
| K1–K7 | K1–K3、K5–K7 各自 Meta、Smart；K4 僅 Smart | K4 僅適用 Smart；選定核心使用可追溯的獨立基線 |
| F1–F9、N、R、X、Z | 按功能、核心及狀態選測；條件分支另記 | 同碼不代表已驗另一活躍核心；不因此要求所有功能重跑 |
| F10 | 純靜態排版／文字檢查可 shared；長任務、取消、重複點擊與終態讀回按受影響核心選測 | 只有明確無 runtime／材料相依的子項可 shared，列出子項與原因 |

同一輪操作可以同時提供 A/E、K、F 等多項證據，但每項均須有對應的前置狀態、
觀測及結論，不需為了填表無意義地重複同一操作，也不能用一張總覽截圖覆蓋全部。

## 8. V：每條路徑結尾的共同成功判定

必須逐層讀回，不以單一 `ok:true`、退出碼或 UI 綠色狀態取代：

| 層 | 必需證據 |
| --- | --- |
| V1 UI／任務 | 起始操作、task ID/PID、階段日誌、terminal result、結束時間；不再 running、鎖釋放，頁面與後端一致 |
| V2 元件／服務 | 精確 Core/LuCI/dnsqualify/Dashboard 身分、Meta/Smart 兩份 binary SHA 及活躍核心；procd MCP instance 與 HTTP health 均正常 |
| V3 材料 | 本輪真實來源摘要、節點／候選／合格數、策略／自訂網站摘要、配置 SHA、材料提交結果；無舊快照冒充新結果 |
| V4 配置／runtime | 由目標核心執行的 `mihomo -t`／attestation 成功；managed PID／實際 executable 身分、flavor、router profile、controller、載入代理組型別與配置相符；Smart 另需 K4 模型載入證據 |
| V5 接管 | LuCI `takeover_status` effective、ownership、TUN、policy rule/route、nft/DNS 檢查與本輪配置一致 |
| V6 網路 | 本情境下重新執行 N1–N4 必要功能斷言，附受控 LAN 請求／回應、接管與可識別直連／代理路徑；外部品質觀察另列，不要求外網零失敗 |

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

配合 Core 建置 commit／產物 SHA、實際 executable SHA、`runtime status --json`、controller `/version`、
`/proxies`、`/rules`、`/connections` 和當輪配置 hash。controller 認證從 VM 安全取得，
不要把 secret 放到命令紀錄或截圖。Mihomo 配置驗證使用產品隔離驗證路徑，
不要手動對正在運行的 Smart workdir 啟動另一個 `mihomo -t` 去爭用 cache.db。
不得假設候選提供 `localclash version` 子命令；版本標籤不能取代上述身分核對。

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

### K：核心差異與狀態

此關卡驗證「目標核心真的以預期方式運作」，不是把相同流程換一個 UI 選項。
K1/K2 證據要附在每條 A–E 結尾；其餘案例可以引用符合前置條件的證據，跨版本沿用依 SOP 判斷適用性。
正常案例按選定斷言取得 V/N；故障及停止階段驗拒絕／保護狀態，恢復後再驗相關 V/N。

| ID | 操作與核心差異 | 通過條件及證據 |
| --- | --- | --- |
| K1 身分閉環 | 分別選用 Meta/Smart；比對 UI 意圖、runtime profile、磁碟 binary、managed PID 的實際 executable 與 controller build | 五層指向同一個受測核心；不只憑檔名、`running:true` 或兩者可能共有的 Mihomo/Meta 版本前綴判定 |
| K2 配置語義 | 同一份完整預設 intent 各生成 Meta、Smart 配置，使用各自 binary 驗證，再讀回 loaded group 型別 | 自動組在 Meta 是 `url-test`，Smart 是 `smart`；Smart 專用參數、地域權重與 runtime defaults 符合該候選；Meta 不殘留前次 Smart 注入的配置 |
| K3 冷／暖啟動 | 兩核心分別首次啟動、正常停止後再次啟動；Smart 冷態無模型／統計，暖態已有有效模型及持久化統計 | 各自記錄 preflight、模型取得／載入、controller ready、takeover ready、首個 LAN 請求及 CPU/RSS；不得把 preflight 成功當正式 runtime 已完成模型載入 |
| K4 模型完整性（Smart） | 有效模型、缺失模型、損壞模型、模型下載失敗各用獨立副本；另驗證模型更新成功及無效更新候選 | 正常主線有實際模型 SHA／載入成功證據；失敗有可定位錯誤，不能把檔案存在或 `type: smart` 當作模型生效；更新不得以壞檔覆蓋有效模型，恢復來源後正式 runtime 能重新載入 |
| K5 狀態／驗證隔離 | 兩核心對刷新、hot reload、process restart、同核心版本升級取前後狀態；Smart 另核對模型、統計、排名／收集資料；活躍 runtime 期間經產品入口配置驗證 | 驗證使用目標核心與隔離 workdir、不爭用 live DB；沒有非預期遺失／格式錯誤／鎖衝突；持久化證據可重新載入，N 保持符合各階段承諾 |
| K6 實際選路 | 固定至少兩個可用且出口可辨識的測試節點與真實 TCP/UDP 流量，分別測正常、某節點失敗、恢復 | Meta 驗證健康檢查、自動組選擇及實際出口；Smart 驗證模型載入、群組／節點選擇及失敗節點處理；保留完整連線 chain、失敗及恢復時序，不要求兩核心選相同或絕對最快節點 |
| K7 成對更新與失敗保護 | 在 Meta 活躍、Smart 活躍下分別更新 Mihomo；依次注入 Meta 候選失敗、Smart 候選失敗及第二個檔案替換失敗 | 兩檔準備／替換狀態可追溯，不能半新半舊卻宣稱成功；當前核心候選依現行配置通過驗證，失敗按交易回復並核對兩檔摘要；成功後保留原核心選擇並 V/N 通過 |

**配置契約：**目前 [renderer](../internal/configrender/render.go) 在 Smart 模式把
`url-test` 轉為 `smart`、移除 `tolerance`，並套用 `policy-priority`、
`uselightgbm`、`prefer-asn`、`collectdata`、`sample-rate` 等有效選項及
`lgbm-*`／`smart-collector-size` defaults。測試記錄候選的實際值和已載入值，
不把所有可選欄位一律當作必有，也不要求 Meta 支援 Smart 專用配置。
基線使用同一 intent 重新渲染，不把 Smart YAML 直接交給 Meta 當成切換。

**模型與暖態：**模型檔位於實際 `-d` workdir，不假設是使用者 home 預設路徑。
K4 注入損壞／缺失僅在已停止的獨立副本進行，不破壞活躍 runtime 的唯一有效模型。
若所測 Smart 版本既有設計在模型故障時改用非模型權重，必須記錄其已核實契約、
錯誤及降級證據；這只能滿足負向案例，不能代替正常主線的 LightGBM 載入通過。
不因此新增或默許 localClash 靜默改用 Meta。欠缺可驗證的模型載入／選路觀測時
記取證 ERROR，不從成功 HTTP 請求反推模型生效。

**持久化判定：**Smart 使用的 `Model.bin`、`cache.db` 及啟用時的
`smart_weight_data.csv` 必須分開記錄。一般資料庫可能持續變動，不能要求活躍
`cache.db` 的整檔 hash 恆定，也不把程序內暫存統計當成已寫入磁碟。先產生固定
流量，等待該候選明確的 flush／正常 shutdown，再在停止的副本核對可讀統計與
重啟後載入；格式遷移或資料淘汰需有已鎖定的規則，不能只看檔案仍在。
Meta 可使用一般 cache，但不應依賴 LightGBM 模型或啟動 Smart 資料收集。

**選路與效能：**K6 使用獨立測試 intent／規則、固定節點與隔離故障，原始完整
主線配置仍須保留。先記錄候選的健康檢查／重試／失敗暫停窗口及允許恢復時間，
不假定兩核心立即切換或算法相同；沒有受控節點／觀測能力就記前置 ERROR，案例 NOT_RUN。
K6 驗證候選的核心／配置整合與受控失敗處理，不保證供應商節點品質、公共網站
可達性或 Smart 必然比 Meta 快；演算法／Mihomo 依賴缺陷與 Core／LuCI 歸屬分列。
K3/K6 分別記錄冷、暖、首次請求與後續請求，不用平均值掩蓋第一筆超時。
CPU/RSS、啟動及請求耗時在相同 QEMU 資源／輸入下比較；退化門檻在 G00 固定，
不得事後放寬，也不把 TCG 結果宣稱為實機效能排名。

**兩檔與當前核心：**[更新交易](../product_mihomo_update.go) 會準備 Meta/Smart
兩檔，但配置 preflight 使用當前選定核心；一次 Meta 更新通過不等於 Smart 配置
已驗證，反之亦然。K7 對兩種活躍核心各有獨立證據；成對更新交易改動時選測兩者；另一個新檔的可運行性由另一核心
的獨立 A/B/K1/K2 證據驗證，不要求不存在的運行中切換。升級及模型更新的前後
身分需分開記錄，不混成一筆版本更新。

## 9. F：一般功能性可用範圍

選定 F 功能時，F3 由獨立 S1 初始化，其餘依需要使用可工作的 S2 副本；
改動後檢查 task、材料、runtime 讀回與對應真實請求，
結束後恢復基線，避免一項的設定影響下一項。依選測計畫展開受影響核心，
不得因 helper 相同就共用 runtime／網路證據。
每個已選操作與負向分支都須個別留結果；任何非預期錯誤立即按[SOP 的結果與異常規則](istoreos-release-test-sop.md#results)
登錄，不可因同一功能另一操作成功、或 workaround 後可用，就省略缺陷。

| ID | 操作 | 通過條件 |
| --- | --- | --- |
| F1 訂閱 CRUD | 加入兩個以上來源及單節點 URI、修改、刪除、保存並應用、刷新；另測空白／結構非法輸入，以及已配置但遠端抓取／解析與來源 cache 均失敗的來源 | 結構非法配置必須明確拒絕且不清空合法狀態；已配置來源的 runtime 抓取／解析／cache 失敗時，若仍有至少一個有效來源則標記 failed、顯示 warning 並跳過，合併／移除與來源歸屬正確；全部來源無效才失敗並保留舊合併結果；重新開頁仍有保存內容 |
| F2 大訂閱 | 固定完整大樣本執行保存／刷新，覆蓋過去 240 秒邊界 | heartbeat 持續、無固定短外層 deadline 誤殺；完成後 V 通過；不能靠無限延長無進度任務過關 |
| F3 策略／核心 | 從獨立 S1 將完整預設及 minimal 分別用 Meta、Smart 初始化 | 四個組合各以 K1/K2 讀回核心與配置語義；minimal 不替代完整主線，不要求產品未提供的 S2 重新初始化或運行中核心切換 |
| F4 网站分流 | 新增／刪除直連與代理域名、子域、萬用字元；同域兩邊與先後新增順序；非法 pattern | 最新成功規則優先、警告與刪除後恢復較舊規則正確；實際命中／出口符合，不只列表更新 |
| F5 runtime／接管 | 啟動、重啟、停止；runtime 保持運行時單獨停止接管，再套用接管 | UI 與後端分開表達兩種狀態；只移除 localClash-owned 規則；無重複規則或 orphan 程序 |
| F6 Dashboard | 從 LuCI 開面板，讀版本、代理組、連線，選擇專用測試組的出口並發請求 | 認證／資源／API 正常，選擇確實生效；不因面板載入成功就算路由通過 |
| F7 MCP | 按頁面接入資訊連 QEMU MCP，完成協定 initialize、tools/list、environment_inspect 及路由唯讀查詢 | 連到正確 guest、工具可呼叫、結果與 LuCI／controller 相符；不是只 GET health |
| F8 DNS 最佳化 | 查看基線，執行 dnsqualify；有合格環境時套用並明確重啟，再刪除設定；另在量測期間改變 WAN device identity 驗證拒絕 | 一般解析與 LAN 名稱正常；可用／不適用／失敗如實顯示；WAN 身分漂移不提交結果；刪除回到加密 DNS 基線，不暗改節點 DNS |
| F9 元件維護 | 分別操作 LuCI、Core、Mihomo、Dashboard 的可見維護入口與 MCP 服務停止／啟動 | 實際版本及服務符合結果；之後仍可初始化／更新與連接 MCP，沒有半更新狀態 |
| F10 UI／任務 | 各頁與狀態區、長任務日誌、重複點擊、重新載入頁面再觀察原任務 | 不並行啟動互斥寫入；不因斷開頁面遺失終態；沒有未處理錯誤或無限 busy |

F8 的成功最佳化子案例，僅在已證明 WAN 不符合產品資格時可 N/A；資格判定、
明確拒絕、一般 DNS、刪除與基線恢復各自追蹤，不因成功分支 N/A 而自動豁免。不能製造假 public IP／假資格當成功。
`expires_at` 已從 dnsqualify 產出與 LuCI 狀態契約移除；Core 僅為讀取舊 v2 文件
保留無執行語義的相容欄位，因此不得建立「證據過期後拒絕」測試或據此阻擋發版。

## 10. 更新／初始化的資料保留矩陣

Meta、Smart 各自從相同核心的 S2 克隆兩份，一份開啟預設策略同步，另一份關閉。
執行計畫選定的 B/D/E 與同步偏好分支；策略同步開啟仍是主線，不因自訂策略會被覆蓋就關掉它。

| 資料 | 預設策略同步開啟 | 預設策略同步關閉 |
| --- | --- | --- |
| 訂閱來源 | 保留並真正刷新，不用舊快照冒充成功 | 相同 |
| 使用者自訂策略補丁 | 按明示確認被最新預設策略重建／覆蓋 | 保留；若舊契約不相容，明確失敗而非偷換策略 |
| 「网站分流」代理／直連列表 | 保留數量、內容摘要與順序；與自訂策略補丁是不同資料 | 相同 |
| 同步策略偏好 | 保存選擇；重新開頁與下一次更新一致 | 相同 |
| 開機恢復意圖 | 不因更新自行啟用／停用 | 相同 |
| 使用者額外資源檔 | 安裝包允諾保留的額外檔案不丟失 | 相同 |
| 活躍核心選擇 | 一般更新與套件升級維持原選擇；核心只在 S1 正式初始化時由使用者選擇 | 相同 |
| 模型／runtime 統計與一般快取 | 依 K4/K5 核對保留、更新或格式遷移；不是使用者策略補丁，不可因策略同步被意外清空 | 相同 |

目前自動選擇組使用完整可選節點，ChatGPT capability 獨立產生。新配置不得再依賴
已移除的 g204 capability；舊 intent 在不更新策略時明確拒絕可作負向 PASS，
但必須接著走同步新版預設策略的支援路徑成功，不能把拒絕本身當完成升級。

## 11. N：受控網路功能與外部品質觀察

### 11.1 必要的網路功能驗證

G01 的 LAN client、受控端點與可識別測試代理是前置條件。每輪記錄實際目的地、
時間、回應及本輪 policy/controller/nft 證據；事先固定探測逾時和允許的切換
中斷預算，不得看到結果後才放寬標準。每種正常狀態至少連續探測三次，保留
全部失敗；三次成功只滿足取樣要求，不替代功能斷言與責任判讀。
N1–N6 的證據綁定實測核心與冷暖狀態；選測核心改變需有該核心的證據，不能借用
另一核心流量。下列是可選的產品功能斷言，N5 的
條件性 N/A 也要分核心記錄原因；公共網路品質不是這些必要斷言的一部分。

| ID | 測試 | 必需結果 |
| --- | --- | --- |
| N1 直連 | LAN client → 受控 WAN 側直連 HTTP/TCP 端點 | 固定 payload 正確回應，符合預期 DIRECT 規則；client ingress、受測轉送路徑及端點身分可對應，不只看 HTTP 200 |
| N2 代理 | 同一 client → 經可識別測試代理到受控端點 | 命中預期代理鏈，端點或代理側證據能區分直連與代理；沒有被錯配置為 DIRECT，不能只靠網站可達性 |
| N3 DNS／區網 | 受控 DNS 查詢／答案、router DNS、固定本地域名／DHCP 名稱、LAN 服務 | 上下游查詢與答案符合配置；需接管與本地 bypass 路徑分別正確，不破壞本地解析／連線；不以公共 DNS 永不逾時作承諾 |
| N4 UDP | client 對受控 WAN 側 UDP echo／DNS 端點發唯一 payload | 預先指定直連／代理意圖，有應用層回應、TUN／路由計數及對應出入流量；TCP 成功不能替代，不把任意公共 UDP 端點當必達服務 |
| N5 IPv6 | 已宣稱支援且具可驗證 IPv6 路徑時重跑 N1–N4 | 正確 IPv6 捕獲／出口／DNS；未配置 IPv6 可 N/A，但報告不可宣稱驗證 IPv6 |
| N6 連續性 | 初始化後、更新各檢查點、重啟／停止／恢復期間持續探測受控端點 | 準備階段不提前破壞舊鏈；在端點健康前提下量測候選切換中斷與恢復，無切換後持續黑洞；外部網站掉包另記 |

明確停止期間不要求代理路徑通過；要驗證已停止及正常直連／管理通道。
只有執行恢復後，才按預先設定的恢復預算要求代理及接管重新可用。

受控端點不能只位於會被直接 bypass 的管理 LAN，必須證明流量走過待驗的
轉送／代理路徑。測試路由使用產品支援的入口，保存完整主線配置與測試意圖；
不靠手改生成 YAML 製造通過。控制面與實際回應都須驗證，不能只驗程序存在。
若仍無法區分受測直連／代理路徑，該必要斷言的取證記 ERROR；若受控證據已足夠，
無法判定宿主更下游的真實 public IP／ISP 品質僅限制外部出口聲稱，不阻擋已驗證
的產品功能。指定 proxy curl 可作輔助診斷，不取代 LAN-forwarded N1–N4。

### 11.2 外部品質觀察及失敗處置

公共網站、公共 DNS、真實訂閱節點另建 `external_observation` 案例，記錄
目的地／地區、TCP／UDP、實際匹配規則、預期路徑、每次回應與耗時；原始
逾時仍是該次觀察 FAIL，不刪除、不因後續成功改寫，也不自動加入產品待修清單。

例如：`www.baidu.com:443` 命中 `GeoSite/cn → DIRECT` 與
`9.9.9.9:9953` 命中 `MATCH → DIRECT` 是不同測試內容；不能把兩者統稱為
「直連失敗」。後者是裸 IP 公共 UDP DNS，先核對測試的路由意圖與當地可達性，
不能由其逾時反推 localClash 有 Bug。前者即使符合直連規則，逾時也仍可能涉及
Mihomo、DNS、QEMU、宿主／ISP 或目的服務，僅有撥號錯誤不足以確定根因。

外部失敗後進行有界核對：

1. 核對候選身分、生成與載入的配置、匹配規則、接管及同時段任務狀態，
   先排查是否有 Core／LuCI 破壞必要功能的直接證據。
2. 以事先準備的受控端點核對相關 N 斷言；必要時比對同次 client／guest
   出入流量、DNS ID、目的回應及端點健康。只有缺哪層證據才追加哪層觀測，
   不為了替一次偶發逾時找根因無限重測，或擅自擴大到宿主／生產網路操作。
3. 若必要功能失效，按[SOP 的歸屬與阻擋規則](istoreos-release-test-sop.md#results)列功能缺陷或證據缺口；如果必要功能已驗證且
   沒有候選回歸證據，保留非阻擋的待定位外部觀察，說明證據與未確認範圍。
   不要求每個外部觀察都找到根因／有修復 commit，才允許結束驗收。

改用受控端點不是把原公共網站 FAIL 改成 PASS；兩者是不同的斷言及結果。
候選功能本身的失敗仍受[SOP 的結果與異常規則](istoreos-release-test-sop.md#results)限制，不能靠 workaround 或事後分類逃避重驗。

## 12. R：真正重啟與恢復

選定案例依受影響核心從獨立 S2-meta／S2-smart 執行，操作前後保存 boot ID、PID、
核心身分、意圖、接管與 N 證據。Smart 另核對模型／持久化載入；不能用 Meta
的重啟或 watchdog 恢復證據代表 Smart。

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

### DNS health lease：接管失效後恢復基線

對應功能表 `DNS-HEALTH-LEASE`，僅在其受影響／缺證據時選測。現行 LuCI
`dns-guard` 與 `takeover-apply` 使用有 timeout 的 nft 集合；主線保留 WAN/dnsmasq，
UDP/TCP 健康探測都成功才續租。這與 dnsqualify 的最佳化資格是不同功能。

選定的健康、探測失敗、guard／Mihomo 退出、generation 改變、明確停止等 variant
分開記結果。先保存候選實際 TTL、探測間隔、generation 及 WAN/dnsmasq 基線；
故障注入限測試副本。核對租約不再延長、到期後集合元素失效與真實 DNS 回應，
不能靠手動 rollback 或修復 callback 代替到期恢復。舊 generation／停止後 worker
不得續租；恢復健康後是否重新接管按實際產品意圖判定。
`lease_active` 只能證明觀察當下的租約狀態，不能單獨證明失效或程序退出後會恢復。

## 13. X：失敗注入、取消與重試

每次只注入一個故障。先列明目標、預期錯誤、前後檔案摘要、允許保留的檢查點、
恢復動作與退出條件；由獨立 overlay 保護其他案例。產品 timeout 不得為過關而修改。
選定的 X 子案例在計畫指定核心及 S1/S2 狀態執行，例如初始化取消
由 S1 開始、運行中更新由 S2 開始；條件性不適用仍需證據。模型故障另見
Smart K4，成對 binary 更新故障見雙核心 K7。
對大訂閱記錄 heartbeat、處理數與最後進度；無進度達本輪預定觀察預算時取證並
取消，依產品斷言或取證情況標 FAIL／ERROR，不無限等待，也不把操作者取消當作產品原生 timeout。

| ID | 注入方式與時機 | 預期與重試 |
| --- | --- | --- |
| X1 下載／校驗 | 測試源暫時失聯、空檔或錯誤 checksum；先測可恢復短故障，再測持續故障 | 在既有次數／逾時內重試或明確失敗；不安裝未驗證檔、不把空輸出當成功；恢復來源後正常更新 |
| X2 訂閱失敗 | 在軟體檢查點完成後，分別讓多來源中的一個受控來源回錯誤／無效內容（另有有效來源），以及讓全部受控來源回錯誤／無效內容 | **mixed partial-success：**至少一個來源有效時材料階段成功；失敗來源明確為 `status: failed`、warning 可見並跳過，不產生其失敗 artifact；只合併健康來源，render／config-test／commit 完成，並核對提交後材料。**all-invalid failure：**全部來源無效時材料階段明確失敗，保留 joined causes、舊合併材料 byte-identical、已提交軟體檢查點及 runtime 不被回寫；修正來源後整條更新成功。兩個子案例都要保留原始失敗事件與前後摘要，不能以後續成功覆寫。 |
| X3 配置／熱載入 | 在測試副本用可控非法材料觸發驗證拒絕；另測驗證後 controller 暫時不可達 | 明確拒絕／失敗，無半套新材料或假成功；核對前後摘要與實際已載入狀態，再恢復並重試 |
| X4 安裝／啟動失敗 | 在專用副本中以受控資源不足或占用目標 listener 觸發失敗 | 定位到安裝／啟動階段；保留可診斷結果，不盲目反覆啟動；移除注入後支援的修復路徑成功 |
| X5 取消 | 初始化與一鍵更新的準備、材料階段分別點取消 | 子程序及任務有終態、鎖釋放；無延遲 worker 又覆蓋狀態；保留合法檢查點，再按正常入口成功 |
| X6 RPC/UI 中斷 | 更新中重新載入頁面／中斷客戶端連線，另觀察套件 re-exec | 可找回同一任務終態，不重複執行；新 helper 與 MCP 身分正確，V/N 通過 |
| X7 不相容舊策略 | 關鍵舊版配置關閉預設策略同步後更新 | 明確拒絕已移除契約，不暗自替代；資料保持可恢復，開啟同步後真正升級成功 |

X7 只在 G00 的支援／遷移範圍包含已移除或不相容契約時執行，並固定能重現該
契約的歷史基線。若本次舊版配置全部相容，附契約差異證據後可標 N/A；不得為了
滿足「應拒絕」而把正常相容升級判失敗。一般非法輸入／驗證拒絕由 F1、F4、X3 對應功能追蹤及按需重驗。

任何恢復先保存故障證據並完成案例／缺陷登錄；未在案例預先列明的補救遵守
[SOP 的結果與異常規則](istoreos-release-test-sop.md#results)，不可自行增加步驟過關。管理頁不可達時使用已確認的 console；不要改宿主網路，
不要照搬生產路由器清理指令。無法安全恢復時停止 guest，保留失敗 overlay，
從乾淨副本再測；失敗紀錄仍保留在本次結果中。

## 14. Z：重置與重新開始

選測重置時，從計畫指定核心、含資料保留樣本的 S2 副本開始，先用產品操作停止接管與
runtime，確認停止成功。
分別驗證 UI 提供的完整 workspace 重置；記錄確認文字與實際刪除計畫。

- 完整 workspace 重置核對 workspace marker 與固定範圍；不刪 LuCI 套件或
  workspace 外的 Core。不能接受任意路徑或刪除其他應用的狀態。
- 重置後 UI 顯示真實未初始化狀態，沒有殘留接管；重新走「开始初始化」及 V/N。
- 記錄模型／統計／快取實際是否位於被重置的 workspace 或 `.runtime/` 範圍；
  應刪除的 Smart 狀態不得殘留，保留的程式不得被誤刪。重置後分別選原核心
  初始化；Smart 以實際無模型／統計狀態重驗 K3/K4，不借用重置前的暖態證據。
- 重置功能本身不能替代 G01 的乾淨 S0 證明；A 仍必須由乾淨韌體開始。
- 現行 LuCI `reset` 只調用 Core 的 `reset --full`；不存在配置專用 reset UI，
  因此不得建立配置單獨重置案例或用不存在的入口阻擋發版。
