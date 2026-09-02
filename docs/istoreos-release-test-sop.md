# iStoreOS QEMU 發版前測試 SOP

狀態：現行驗收規範。制定日期：2026-09-03。
核心覆蓋：Meta、Smart 及兩個方向的核心切換均為獨立必驗結果。

本文件是 localClash Core 與 localclash-luci **唯一的發版功能驗收 SOP**。
每次發布任一專案，都要鎖定配對版本，在真正的 iStoreOS QEMU 韌體中完成
任務鏈；不是只確認 CI 綠燈、安裝包可解壓、UI 正常或程序存活。
文件存在不表示某個版本已通過；每次執行必須另有綁定產物的驗收紀錄。
目的是逐項發現並報告受測功能的缺陷，不是靠補救把流程跑到最後。
預設驗收目標是發版任務開始時鎖定的 **HEAD commit 候選**；歷史正式版本只是
升級起點。兩者的錯誤都要報告，但不得混成同一版本的驗收結果。
所有異常都要記錄；「請求失敗」「已確認產品缺陷」「阻擋發版」是三個獨立判定。
本 SOP 驗證 localClash／LuCI 的功能與所選核心整合，不保證公共網際網路品質。

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
- Mihomo 負責實際代理／直連撥號及核心內部處理；節點供應商、DNS 上游、目的
  服務、QEMU／宿主網路各有獨立邊界。外部逾時不能直接算作 Core／LuCI 缺陷，
  也不能只因看到 Mihomo 錯誤日誌就斷言核心有 Bug。所選核心若在受控環境確實
  破壞必要功能，仍屬候選整合阻擋，但不要求必須修改 localClash 程式才能解決。
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
  → A / B / C / D / E（每條各跑 Meta、Smart，結尾執行 V/N）
  → K 核心差異、狀態與雙向切換
  → F 一般功能 → N 實際網路 → R 重啟恢復 → X 失敗與重試
  → Z 重置再初始化 → G99 證據審核與發版判定
```

每項必填：ID、core_scope、前置狀態、輸入、操作、預期、實測、證據、耗時、
結果、恢復方式。core_scope 為 `meta`、`smart`、`meta-to-smart`、
`smart-to-meta` 或本文件允許的 `shared`；不能以一筆「Mihomo 通過」代表全部。
結果只有 `PASS`、`FAIL`、`BLOCKED`、`N/A`：

每項另填 `acceptance_scope`（`required_function` 或 `external_observation`），
每筆異常另填歸屬、信心與阻擋理由。結果記錄實測，範圍決定是否納入放行判定；
不得因外部觀察 FAIL，就將已獨立驗證的產品功能或整個核心結果改為 FAIL。

- 預期拒絕的負向案例，在明確拒絕且狀態保護正確時才是 PASS。
- 未執行、環境不具備、只有模擬結果或缺少證據，一律 BLOCKED，不是 PASS。
- A–E、K、V、IPv4 的 N、R、X、Z 不得因時間或既有故障而標 N/A。
  只有本文明列的條件分支（F8 最佳化成功、N5 IPv6、X7 不相容舊策略）可 N/A，
  且必須附不適用證據。
- 已知問題不自動豁免。必要功能關卡 FAIL／BLOCKED 時不能放行；外部品質觀察
  不要求全部成功。報告漏列異常仍阻擋 G99，已完整記錄的非阻擋異常則不等於漏測。
- 必要功能關卡失敗時停止依賴該成功前提的下游，標為 BLOCKED 並引用異常／缺陷 ID；
  可以在另一合格的獨立基線採集其他案例，但不能沖銷失敗或將補救副本當正常 S2。
- 測試中修正程式或替換候選資產，產生新的候選識別並重跑受影響的情境、V、
  N 及相關 R/X；若變更初始化、更新或共用材料交易，重跑雙核心 A–E。
  任一 Mihomo binary、Smart 模型或核心選擇／渲染契約改變，也重跑受影響核心的
  K、V/N 及兩方向 K8；成對更新邏輯改變時 K7 必須在兩種活躍核心下重跑。
- 發布責任人審核 G99 前，不推送會觸發公開發布的 tag、不發布或廣播正式 Release。
  現有 CI **沒有自動執行／強制核驗 QEMU 報告**；這是人工放行關卡，不能稱為自動門禁。

### 2.1 每個功能的失敗必須立即登錄

1. 每個功能及其子案例都要有獨立 ID、每次嘗試的 attempt ID、預期與結果。
   範圍包含候選功能、歷史基線建立、安裝／交接、恢復及測試工具；不得只列最終
   成功的主線。父項或整輪已 FAIL／BLOCKED，也不能省略後來發現的另一項缺陷。
2. 發生非預期失敗，先保留原始 task／錯誤碼、完整錯誤訊息、時間、版本身分及
   前後狀態，再立即建立異常 ID，連到案例與證據，並向使用者報告。已確認功能
   缺陷另連缺陷 ID；歸屬未明先列待定位，不能直接命名為 localClash 待修 Bug。
   不得等補救成功才決定是否報告，也不得只藏在 log、備註或子代理交接訊息內。
3. 正常功能偏離預期即記 FAIL；未執行、身分不明或觀測工具使結果不可判讀記
   BLOCKED。根因未明仍須報告症狀與影響，分類可暫為「待定位」，不可先歸咎上游。
   每個非預期失敗事件都要入索引；若多次事件屬於同一問題，可關聯同一問題 ID，
   但不得丟失各次版本、時間及嘗試紀錄。
4. 本來就預期拒絕的負向案例按既定斷言判定；產品內建重試／備援來源若符合
   事先鎖定的契約，可按該契約判定成功。所有中途錯誤、重試與 warning 仍要
   索引並說明判定理由；不能事後把非預期失敗改稱負向測試或正常重試。
5. 報告逐項標明候選產品、歷史版本、核心依賴、外部服務、測試工具、環境或
   SOP／能力不一致問題，並區分已確認與待定位。記錄異常不等於要求新增修復 commit。
   「舊版已有」「今天已修」「其他案例成功」均不是漏報理由；已知修復必須連到
   owning repo 的 commit，核對受測檔案是否包含它，並另附新候選重驗結果。
6. 一般測試授權不包含任意 workaround。首次初始化失敗後，不得自行追加
   「保存／應用訂閱」、手動 refresh、預填 capability、換模板、清 cache 或重試，
   再把它算作首次初始化成功。先登錄 FAIL 並停止該正常路徑；需要額外補救時，
   先說明目的、範圍及結果限制並取得明確授權，在另存的副本／嘗試中執行。
7. SOP 已明列的負向案例恢復步驟可按原計畫執行；計畫外 workaround 則另記
   操作、授權、狀態變更與結果。兩者都不能覆寫原始結果，或自動恢復正常基線資格。
   補救後只能標「補救／診斷副本」，其後續成功不替代正常初始化或正常 S2 升級。
   修復後須從該案例原定的合格基線、不經額外補救重跑，才有新的 PASS 證據。

例如：舊版首次初始化報 `ChatGPT capability snapshot ... is unavailable`，
必須列為該舊版的初始化 FAIL。之後保存／應用能運行，只能另列補救成功；
不能省略缺陷，也不能因此宣稱舊版首次使用正常、候選回歸失敗或取得正常 S2。

### 2.2 工具與結果彙整不得吞掉失敗

- 分開保存 transport／觀察工具退出碼、產品 task `exit_code`、內層 `ok/error`、
  檢查點與功能斷言。HTTP 200、task_status 的 `ok:true`、成功取得終態及新 PID，
  都不代表受測功能 PASS。
- 案例 runner 遇非預期產品失敗或斷言失敗，必須回傳非零退出碼。若純觀察器以
  0 表示「已取得終態」，上層必須解析明確的產品／案例結果；不得只看觀察器退出碼。
  欄位缺失、結果無法解析、未取得終態、工具逾時都不得預設成功。
- 外部觀察的單項 runner 同樣保留失敗退出碼；總彙整按 `acceptance_scope`、
  必要斷言及有證據的 `release_blocking` 判定放行，不將所有子程序退出碼直接
  合併成產品 FAIL，也不為了避免阻擋而把外部失敗退出碼改成 0。
- 單筆請求失敗不得被後面的成功命令或 shell 最後一次退出碼掩蓋。各次請求、
  任務和人工操作均有嘗試紀錄；補救、重試、取消都不能覆寫原始失敗檔。
- 收尾將原始終態、請求結果、工具錯誤、UI 錯誤／warning 與案例／異常／缺陷索引逐筆
  對帳。每個事件須指向異常／缺陷，或有可查證的預期拒絕／產品內建恢復判定；只做
  關鍵字搜尋、只讀父任務成功、或只人工列主要 Bug，都不足以完成對帳。
- 未映射錯誤、漏列缺陷或沒有結果的已執行案例數必須為零，否則 G99 BLOCKED，
  不得聲稱報告已完整。可以提交「尚未完成」報告，但須列出遺漏及待查範圍。
  這些是工具必須滿足的契約，不表示現有腳本／CI 已實作自動收集與強制攔截。

### 2.3 歸屬與放行分開判定

| 已有證據 | 紀錄與責任 | 放行影響 |
| --- | --- | --- |
| 配置／訂閱材料錯誤、錯核心、任務假成功、生命週期或接管不符合契約 | Core／LuCI 功能缺陷，指定 owning repo | 必要功能 FAIL，阻擋 |
| 正確配置下，所選 Mihomo 在受控端點可重現必要功能失效 | 核心依賴／整合問題；不能自動歸責 Core 程式 | 候選必要整合 FAIL，阻擋；可修依賴或調整候選，無須湊 Core 修復 commit |
| 工具／環境不可用，無法取得必要功能斷言 | 測試證據缺口，不先稱產品 Bug | 只阻擋未驗證的必要關卡；先修測試環境或建立合格受控證據 |
| 受控必要功能已驗證，公共網站／外部節點偶發失敗，無候選破壞功能的證據 | 外部觀察異常；根因可仍未明，不擅自歸責 Mihomo 或上游 | 異常保留、單獨說明，不僅因它未解根因而阻擋發布 |

是否阻擋取決於「哪個必要功能未成立／未驗證」，不是是否已查清每筆外網逾時。
確認必要功能違反契約不必等到定位某行程式；根因或 owning repo 仍待定位時，
該功能的失敗與阻擋仍保留，不能因此降為非阻擋外部觀察。
不要求以修復 localClash、增加重試、放寬 timeout 或新增 commit 結束外部品質調查。
但不能把已重現的產品功能故障改稱外部觀察；單有 running、controller ready 或
`DIRECT` chain，也不足以證明所有配置／接管正確。

在 G00/G01 預先指定必要斷言、受控端點與外部觀察範圍；不得看到 FAIL 後偷偷
降級範圍或換較容易的網站取代原結果。若發現測試設計把外部品質錯當產品功能，
明列設計缺陷、調整理由與新案例對應，保留舊結果，經審核再以合格副本補驗必要
斷言。這是修正測試設計，不是補救產品或把舊 FAIL 改成 PASS。

## 3. G00：鎖定候選、舊版與前置檢查

### 3.1 受測目標、歷史起點與實際身分

- 預設從待發布專案在本輪開始時的 HEAD 建置未發布候選，鎖定 commit 與產物
  SHA；若使用者指定其他 ref，須明列，不再稱最新 HEAD。Core-only／LuCI-only
  的未發版元件按下列配對規則固定；兩者一起驗收時分別鎖定兩個 HEAD。
- 版本號只是安裝／更新協定的標籤，不是 source commit 證明。不允許以公開
  latest Release 代替候選，也不要求先正式發布才能測試。測試期間不追逐漂移的
  latest；修復後鎖定新 commit、重建新候選並按第 2 節重驗。
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
  若本輪要求驗證內建 HEAD 的新 bundle 而無法提供，該安裝關卡 BLOCKED。
- 歷史正式版本只在 C/D/E 的明示起點使用；其初始化失敗也按第 2.1 節登錄。
  已含修復的新候選必須另從合格基線重驗；不得拿舊版 FAIL 直接宣稱新 HEAD
  仍有同一 Bug，也不能只因 commit 存在就宣稱已通過。歷史缺陷保留追溯，
  新候選的放行看其實測結果及必需升級基線是否合格，不永久繼承舊版 FAIL。

### 3.2 候選與前置檢查清單

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
6. Meta 與 Smart 分別記錄下載來源／解析後 URL、release/build 字串、來源 commit
   （若資產提供）、架構、binary SHA-256，及舊／新版本配對；不可假定二者來自
   相同上游或版本編號。Smart 額外記錄模型來源、SHA、更新設定和資料格式資訊。
   模型自動更新保持產品預設行為，但記錄每次實際變更；測試期間模型改變就分開
   證據批次並重驗受影響項，不以同一模型的結果混算。

**通過：**來源與資產可追溯，前置檢查通過，舊版能建立真實基線。
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
  啟動方式與成功進入 guest LAN 的證據；做不到就 BLOCKED，不能略過 N。
- 必要功能使用受控 WAN 側 HTTP/TCP、UDP echo／DNS 回應端及可識別出口的
  測試代理；預先驗證端點健康、固定預期 payload／答案、路由意圖、逾時與探測
  間隔。請求須由 LAN client 經受測 router／Mihomo，不可用純 LAN bypass
  或宿主 mock 成功取代。受控環境可用隔離 QEMU 拓撲，不要求裸 ISP WAN。
- 公共網站、公共 DNS、真實訂閱節點的可達性／延遲另列外部觀察，不能拿
  「三次皆成功」當成必要功能的唯一斷言。記錄其地區、協定、預期直連／代理
  及選用理由，不能假設所有裸 IP 都應 DIRECT 或所有海外服務都可直接到達。
- 受控端點與可識別測試代理是需具備的測試資源，不宣稱現有 VM 腳本已提供。
  缺少它們時列出具體缺失的功能證據，不要求修改 localClash 來修復外網品質。

### 4.3 測試資料與三種基線

| 基線 | 必要狀態 |
| --- | --- |
| S0 乾淨韌體 | 無 localClash 套件、訂閱、runtime、接管；只有必要管理／測試網路設定 |
| S1 已安裝未初始化 | 套件／bundle 自帶 binary 和資源可存在；沒有訂閱、生成配置、能力快照、使用者策略或接管 |
| S2 已初始化 | 由指定版本的正式 UI 流程生成，V/N 必要功能通過；保存精確版本及狀態指紋，外部品質觀察另列 |

建立 S0/S1/S2 本身也要有案例與嘗試紀錄。基線建立失敗不是可省略的準備雜訊；
受影響的 D/E 等關卡不得接著當正常路徑執行。可使用另一份有來源與成功證據的
既存正常快照，不能把失敗後人工補救的工作區重新命名為合格 S2。

S1 為各核心的獨立副本；S2 必須區分 `S2-meta`、`S2-smart` 及舊／新版。
Smart 再區分未預置模型／統計的冷啟動基線，以及已成功載入模型、有受控流量與
已持久化統計的暖啟動基線。Meta 也分首次啟動與已有一般快取的再次啟動。
不得把 Meta 的 S2 改個名稱當作 Smart 基線；兩者相互切換另由 K8 驗收。
Smart 冷啟動不得從其他 VM 或已運行的 runtime 預先複製 `Model.bin`、統計或 cache。

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

完整預設策略為主線；不可用 minimal 或取消預設策略同步替代。**A–E 每條都必須
在 Meta、Smart 獨立基線執行**，包括 C/D 的舊版升級初始化，不再允許單核心代測。
每條都由真正 LuCI 操作開始，CLI／ubus 用於取證，不替代使用者入口。

| ID | 前置與操作順序 | 必須通過的結果 |
| --- | --- | --- |
| A | S0 → G03 候選安裝 → S1 → 填入完整訂閱 →「开始初始化」→ V/N | 無殘留依賴；元件、訂閱、能力、策略、配置、runtime、接管全鏈完成 |
| B | 本輪候選 S2 →「一键更新」→ V/N → 再次「一键更新」→ V/N | 同版本可重跑、不意外降級、不重複啟動、不留任務鎖；不是只測版本檢查按鈕 |
| C | 舊版 S1 → 使用受支援的套件升級入口裝候選 LuCI → 讀回版本 → 新版 UI 初始化 → V/N | 升級不假造配置；舊套件狀態下新版能完成全新初始化 |
| D | 舊版 S2＋資料保留樣本 → 套件升級候選 → 新版 UI 重新初始化 → V/N | 既有配置遷移正確；策略重建與資料保留符合第 10 節，不被舊 capability 卡住 |
| E | 舊版 S2＋資料保留樣本 → 從舊版 UI「一键更新」取得候選 → V/N | 真正跨版 helper 交接、Core/MCP 更新、兩個檢查點、使用者資料與網路均正確 |

A/C 的 Meta、Smart 分支分別選擇目標核心；D/E 主線保持原核心種類，驗證
`old-meta → new-meta`、`old-smart → new-smart`。跨核心變更不能取代這兩條
同核心升級證據；`meta → smart` 與 `smart → meta` 另外按 K8 執行。
每個選定舊版配對都展開 C/D/E；不能把上一版 Meta 與更舊版 Smart 拼成一組通過。

C/D 記錄使用 `.run` 或 LuCI 套件更新入口，不混寫為 E。E 不得預先覆蓋新版 helper。
若關鍵舊版沒有交接機制，按該版支援的路徑先「检查 LuCI 更新」並完成獨立更新，
刷新頁面後再一鍵更新；報告標記「兩步升級」，不能宣稱直接一鍵升級。

另外從 S1 執行一次一鍵更新：元件可更新，沒有訂閱的材料階段應明確跳過，
不得宣稱完成初始化或自行啟動接管；接著輸入訂閱仍能完成 A 的初始化後半段。
從「已配置但使用者已停止 runtime／接管」的 S2 副本更新，也不得擅自重新開啟。
上述未初始化／已停止的更新分支也分別記錄 Meta、Smart 的結果。

### 核心覆蓋矩陣

| 關卡 | 必須展開的執行範圍 | 共用證據限制 |
| --- | --- | --- |
| G00–G02 | 記錄兩種核心、模型與各自基線／更新來源 | 韌體、管理 LAN 等相同設定可 shared；核心資產與基線不可合併 |
| G03 | 安裝包完整性、真實離線安裝及拒絕邊界 | 尚未選用／啟動核心，可 shared；不代表任一核心已可運行 |
| A–E、V | 每條 Meta、Smart；C/D/E 再乘以選定舊版配對 | 不得 shared |
| K1–K8 | K1–K3、K5–K7 各自 Meta、Smart；K4 僅 Smart，K8 兩方向 | K4 不建立 Meta 模型案例，不以 N/A 免除 Smart 必測項 |
| F1–F9、N、R、X、Z | 分別 Meta、Smart；保留各章明列的條件分支規則 | DNS、訂閱 probe、元件更新即使共用程式碼，也不能代替另一活躍核心的端到端結果 |
| F10 | 純靜態排版／文字檢查可 shared；長任務、取消、重複點擊與終態讀回兩核心必測 | 只有明確無 runtime／材料相依的子項可 shared，列出子項與原因 |

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

### K：核心差異、狀態與雙向切換

此關卡驗證「目標核心真的以預期方式運作」，不是把相同流程換一個 UI 選項。
K1/K2 證據要附在每條 A–E 結尾；其餘案例可以引用符合前置條件的同輪證據。
正常案例需完整 V/N；故障及停止階段則驗證其明列的拒絕／保護狀態，恢復後才跑 V/N。

| ID | 操作與核心差異 | 通過條件及證據 |
| --- | --- | --- |
| K1 身分閉環 | 分別選用 Meta/Smart；比對 UI 意圖、runtime profile、磁碟 binary、managed PID 的實際 executable 與 controller build | 五層指向同一個受測核心；不只憑檔名、`running:true` 或兩者可能共有的 Mihomo/Meta 版本前綴判定 |
| K2 配置語義 | 同一份完整預設 intent 各生成 Meta、Smart 配置，使用各自 binary 驗證，再讀回 loaded group 型別 | 自動組在 Meta 是 `url-test`，Smart 是 `smart`；Smart 專用參數、地域權重與 runtime defaults 符合該候選；Meta 不殘留前次 Smart 注入的配置 |
| K3 冷／暖啟動 | 兩核心分別首次啟動、正常停止後再次啟動；Smart 冷態無模型／統計，暖態已有有效模型及持久化統計 | 各自記錄 preflight、模型取得／載入、controller ready、takeover ready、首個 LAN 請求及 CPU/RSS；不得把 preflight 成功當正式 runtime 已完成模型載入 |
| K4 模型完整性（Smart） | 有效模型、缺失模型、損壞模型、模型下載失敗各用獨立副本；另驗證模型更新成功及無效更新候選 | 正常主線有實際模型 SHA／載入成功證據；失敗有可定位錯誤，不能把檔案存在或 `type: smart` 當作模型生效；更新不得以壞檔覆蓋有效模型，恢復來源後正式 runtime 能重新載入 |
| K5 狀態／驗證隔離 | 兩核心對刷新、hot reload、process restart、同核心版本升級取前後狀態；Smart 另核對模型、統計、排名／收集資料；活躍 runtime 期間經產品入口配置驗證 | 驗證使用目標核心與隔離 workdir、不爭用 live DB；沒有非預期遺失／格式錯誤／鎖衝突；持久化證據可重新載入，N 保持符合各階段承諾 |
| K6 實際選路 | 固定至少兩個可用且出口可辨識的測試節點與真實 TCP/UDP 流量，分別測正常、某節點失敗、恢復 | Meta 驗證健康檢查、自動組選擇及實際出口；Smart 驗證模型載入、群組／節點選擇及失敗節點處理；保留完整連線 chain、失敗及恢復時序，不要求兩核心選相同或絕對最快節點 |
| K7 成對更新與失敗保護 | 在 Meta 活躍、Smart 活躍下分別更新 Mihomo；依次注入 Meta 候選失敗、Smart 候選失敗及第二個檔案替換失敗 | 兩檔準備／替換狀態可追溯，不能半新半舊卻宣稱成功；當前核心候選依現行配置通過驗證，失敗按交易回復並核對兩檔摘要；成功後保留原核心選擇並 V/N 通過 |
| K8 雙向切換 | 各自從健康 S2 開始執行 `meta-to-smart`、`smart-to-meta`；每方向都有成功及目標核心啟動失敗案例，最後切回原核心 | 重新生成目標相容配置、使用目標核心驗證並更換程序；舊程序退出，沒有雙 runtime／埠／DB 衝突；選擇、模型／狀態、接管與流量讀回一致；失敗不假報目標運行或暗換另一核心 |

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
標 BLOCKED，不從成功 HTTP 請求反推模型生效。

**持久化判定：**Smart 使用的 `Model.bin`、`cache.db` 及啟用時的
`smart_weight_data.csv` 必須分開記錄。一般資料庫可能持續變動，不能要求活躍
`cache.db` 的整檔 hash 恆定，也不把程序內暫存統計當成已寫入磁碟。先產生固定
流量，等待該候選明確的 flush／正常 shutdown，再在停止的副本核對可讀統計與
重啟後載入；格式遷移或資料淘汰需有已鎖定的規則，不能只看檔案仍在。
Meta 可使用一般 cache，但不應依賴 LightGBM 模型或啟動 Smart 資料收集。

**選路與效能：**K6 使用獨立測試 intent／規則、固定節點與隔離故障，原始完整
主線配置仍須保留。先記錄候選的健康檢查／重試／失敗暫停窗口及允許恢復時間，
不假定兩核心立即切換或算法相同；沒有受控節點／觀測能力就 BLOCKED。
K6 驗證候選的核心／配置整合與受控失敗處理，不保證供應商節點品質、公共網站
可達性或 Smart 必然比 Meta 快；演算法／Mihomo 依賴缺陷與 Core／LuCI 歸屬分列。
K3/K6 分別記錄冷、暖、首次請求與後續請求，不用平均值掩蓋第一筆超時。
CPU/RSS、啟動及請求耗時在相同 QEMU 資源／輸入下比較；退化門檻在 G00 固定，
不得事後放寬，也不把 TCG 結果宣稱為實機效能排名。

**切換操作：**使用該候選已支援的 LuCI 核心選擇／重新初始化入口；若會重建
策略，先確認範圍、保留測試輸入並核對第 10 節。切換核心需要實際目標程序接替，
只熱載入配置或修改 UI 選項不算完成。每次成功後執行 K1/K2、V/N，記錄切入時
實際冷／暖狀態的 K3 觀測，不為製造冷態而清除切換保留的資料；冷暖完整對照由
K3 的獨立基線提供。另重啟 guest 核對核心選擇持久化（啟用 boot restore 後測 R1，
再恢復原意圖）。目標啟動失敗時，
記錄仍有效的檢查點／明確停止狀態；按已確認的產品恢復入口回到原核心，再驗 V/N。
若沒有支援的切換入口或安全恢復路徑，就標 K8 BLOCKED，不能用手換 binary 冒充。

**兩檔與當前核心：**[更新交易](../product_mihomo_update.go) 會準備 Meta/Smart
兩檔，但配置 preflight 使用當前選定核心；一次 Meta 更新通過不等於 Smart 配置
已驗證，反之亦然。K7 必須在兩種活躍核心各跑，且成功後以 K8 驗證另一個新檔
實際可用。升級、切換、模型更新的前後身分需分開記錄，不混成一筆版本更新。

## 9. F：一般功能性可用範圍

每項由可工作的 S2 副本開始；改動後檢查 task、材料、runtime 讀回與對應真實請求，
結束後恢復基線，避免一項的設定影響下一項。按第 7 節矩陣展開雙核心，
不得因 helper 相同就共用 runtime／網路證據。
每項列出的各個操作與負向分支都須個別留結果；任何非預期錯誤立即按第 2.1 節
登錄，不可因同一功能另一操作成功、或 workaround 後可用，就省略缺陷。

| ID | 操作 | 通過條件 |
| --- | --- | --- |
| F1 訂閱 CRUD | 加入兩個以上來源及單節點 URI、修改、刪除、保存並應用、刷新；另測空白／非法輸入 | 合併／移除與來源歸屬正確；錯誤不清空合法狀態；重新開頁仍有保存內容 |
| F2 大訂閱 | 固定完整大樣本執行保存／刷新，覆蓋過去 240 秒邊界 | heartbeat 持續、無固定短外層 deadline 誤殺；完成後 V 通過；不能靠無限延長無進度任務過關 |
| F3 策略／核心 | 完整預設及 minimal 都分別用 Meta、Smart 初始化／重新初始化 | 四個組合各以 K1/K2 讀回核心與配置語義；minimal 不替代完整主線，核心切換另按 K8 |
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

Meta、Smart 各自從相同核心的 S2 克隆兩份，一份開啟預設策略同步，另一份關閉。
分別執行 B/E；
策略同步開啟仍是主線，不因自訂策略會被覆蓋就關掉它。D 另外核對 UI 的重置承諾。

| 資料 | 預設策略同步開啟 | 預設策略同步關閉 |
| --- | --- | --- |
| 訂閱來源 | 保留並真正刷新，不用舊快照冒充成功 | 相同 |
| 使用者自訂策略補丁 | 按明示確認被最新預設策略重建／覆蓋 | 保留；若舊契約不相容，明確失敗而非偷換策略 |
| 「网站分流」代理／直連列表 | 保留數量、內容摘要與順序；與自訂策略補丁是不同資料 | 相同 |
| 同步策略偏好 | 保存選擇；重新開頁與下一次更新一致 | 相同 |
| 開機恢復意圖 | 不因更新自行啟用／停用 | 相同 |
| 使用者額外資源檔 | 安裝包允諾保留的額外檔案不丟失 | 相同 |
| 活躍核心選擇 | 一般更新維持原選擇；明確重新初始化／切換才依新選擇變更 | 相同 |
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
N1–N6 分別綁定 Meta／Smart 與冷暖狀態；每次 K8 切換後重跑 N1–N4，
不能借用切換前另一核心的流量結果。以下均為 `required_function`，N5 的
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
若仍無法區分受測直連／代理路徑，該必要斷言 BLOCKED；若受控證據已足夠，
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
3. 若必要功能失效，按第 2.3 節列功能缺陷或證據缺口；如果必要功能已驗證且
   沒有候選回歸證據，保留非阻擋的待定位外部觀察，說明證據與未確認範圍。
   不要求每個外部觀察都找到根因／有修復 commit，才允許結束驗收。

改用受控端點不是把原公共網站 FAIL 改成 PASS；兩者是不同的斷言及結果。
候選功能本身的失敗仍受第 2.1 節限制，不能靠 workaround 或事後分類逃避重驗。

## 12. R：真正重啟與恢復

每個案例分別從獨立 S2-meta／S2-smart 執行，操作前後保存 boot ID、PID、
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

## 13. X：失敗注入、取消與重試

每次只注入一個故障。先列明目標、預期錯誤、前後檔案摘要、允許保留的檢查點、
恢復動作與退出條件；由獨立 overlay 保護其他案例。產品 timeout 不得為過關而修改。
X1–X7 各自在 Meta／Smart 情境及該案例指定的 S1/S2 狀態執行，例如初始化取消
由 S1 開始、運行中更新由 S2 開始；條件性不適用仍需證據。模型故障另見
Smart K4，成對 binary 更新故障見雙核心 K7，切換目標啟動失敗見雙方向 K8。
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

任何恢復先保存故障證據並完成案例／缺陷登錄；未在案例預先列明的補救遵守
第 2.1 節，不可自行增加步驟過關。管理頁不可達時使用已確認的 console；不要改宿主網路，
不要照搬生產路由器清理指令。無法安全恢復時停止 guest，保留失敗 overlay，
從乾淨副本再測；失敗紀錄仍保留在本次結果中。

## 14. Z：重置與重新開始

分別從含資料保留樣本的 S2-meta／S2-smart 副本開始，先用產品操作停止接管與
runtime，確認停止成功。
分別驗證 UI 提供的配置重置和完整 workspace 重置；記錄確認文字與實際刪除計畫。

- 配置重置清除訂閱／生成配置／runtime 狀態等宣告範圍，保留套件、下載程式及
  約定保留的 `localclash-user.json`。不得自行擴大範圍。
- 完整 workspace 重置核對 workspace marker 與固定範圍；不刪 LuCI 套件或
  workspace 外的 Core。不能接受任意路徑或刪除其他應用的狀態。
- 重置後 UI 顯示真實未初始化狀態，沒有殘留接管；重新走「开始初始化」及 V/N。
- 記錄模型／統計／快取實際是否位於被重置的 workspace 或 `.runtime/` 範圍；
  應刪除的 Smart 狀態不得殘留，保留的程式不得被誤刪。重置後分別選原核心
  初始化；Smart 以實際無模型／統計狀態重驗 K3/K4，不借用重置前的暖態證據。
- 重置功能本身不能替代 G01 的乾淨 S0 證明；A 仍必須由乾淨韌體開始。

## 15. G99：證據模板與發版放行

每輪在 `.runtime/istoreos-acceptance/<run-id>/` 保存一份執行紀錄及脫敏證據。
`run-id` 包含日期和候選識別；不要把歷史目錄中的 success.json 當作本輪結果。
報告必須同時包含逐功能／子案例結果、異常索引、已確認缺陷清單、補救操作、未執行項及
錯誤對帳結果。摘要不能只列主要 Bug；每項非預期失敗都可從摘要索引追到其
版本、嘗試及證據。報告完整、案例結果與候選可發布分開判定：必要功能 FAIL
不能放行；外部觀察 FAIL 可與必要功能 PASS 並存，須附不阻擋的理由及證據。
未完成的報告不能假稱已找出所有缺陷；根因未知也不等於報告必須無限保持未完成。
以下是紀錄模板，不是另一份 SOP：

```yaml
run_id: <date-and-candidate-id>
sop_revision: <core-commit-containing-this-sop>
operator: <reviewer>
candidate:
  core: {commit: <sha>, version: <version>, artifact_sha256: <sha256>}
  luci: {commit: <sha>, version: <version>, artifact_sha256: <sha256>}
  mihomo:
    meta: {source: <url>, version: <build>, binary_sha256: <sha256>}
    smart: {source: <url>, version: <build>, binary_sha256: <sha256>}
  smart_model: {source: <url>, sha256: <sha256>, update_events: <evidence-path>}
  dependencies_manifest: <restricted-evidence-path>
  update_source_evidence: <redacted-path>
environment:
  firmware_sha256: <sha256>
  overlay_baselines: <S0-S1-S2-identities>
  wan_and_lan_topology: <redacted-path>
inputs:
  old_version_pairs: <exact-pairs>
  old_mihomo_pairs: <per-flavor-versions-and-hashes>
  subscriptions_digest: <digest-not-urls>
  endpoint_expectations_and_timeouts: <path>
cases:
  - id: <A-meta-or-K8-meta-to-smart>
    attempt_id: <unique-attempt-id-never-overwritten>
    core_scope: <meta-smart-meta-to-smart-smart-to-meta-or-shared>
    acceptance_scope: <required_function-or-external_observation>
    execution_role: <candidate-acceptance-historical-baseline-or-recovery-diagnostic>
    candidate_id: <locked-candidate-id>
    vm_id: <overlay-id-and-management-port>
    prerequisite_case_ids: [<qualified-baseline-case>]
    actual_versions_before: <core-commit-sha-luci-helper-sha-and-package-version>
    actual_versions_after: <disk-and-running-identity-or-unavailable>
    state: <cold-warm-stopped-or-not-applicable>
    before: <baseline-and-hashes>
    operation: <UI-operation-and-task-id>
    expected: <state-and-network>
    actual: <observed-result>
    runtime_evidence:
      executable_sha256: <actual-running-binary-sha256-or-stopped>
      controller_build: <observed-build-or-unavailable>
      model_sha256: <actual-smart-model-sha256-or-not-applicable>
      model_load_evidence: <path-or-not-applicable>
    evidence: [<logs>, <hashes>, <runtime>, <takeover>, <requests>]
    duration_seconds: <number>
    result: <PASS-FAIL-BLOCKED-N/A>
    reason: <required-for-non-PASS>
    anomaly_ids: [<anomaly-id-for-each-unexpected-failure>]
    defect_ids: [] # Confirmed defects only; empty does not erase anomaly records.
    release_blocking: <true-or-false-with-evidence-not-inferred-from-result-alone>
    blocking_reason: <unmet-required-assertion-or-evidence-for-nonblocking-disposition>
    workaround_applied: false
    eligible_as_normal_baseline: <true-only-with-required-baseline-proof>
    recovery_and_retest: <result-or-not-needed>
anomalies:
  - id: <unique-anomaly-id>
    case_attempt_ids: [<associated-attempt-ids>]
    observed_at: <timestamp-with-timezone>
    actual_versions: <fault-time-identities>
    symptom: <exact-redacted-error-and-observed-impact>
    owner: <core-luci-mihomo-external-service-test-tool-environment-or-unknown>
    attribution_confidence: <confirmed-suspected-or-unknown>
    attribution_evidence: [<evidence-or-explicit-gap>]
    defect_ids: [] # Link a confirmed defect if established; no invented fix obligation.
    required_assertions: [<affected-function-assertions-or-none>]
    release_blocking: <true-or-false>
    disposition: <blocking-defect-evidence-gap-or-nonblocking-observation>
    disposition_reason: <evidence-and-remaining-unknowns>
defects:
  - id: <unique-defect-id>
    case_attempt_ids: [<all-associated-attempt-ids>]
    category: <candidate-product-historical-product-mihomo-dependency-tool-environment-or-sop>
    anomaly_ids: [<supporting-anomaly-ids>]
    function: <affected-function-and-subcase>
    observed_at: <timestamp-with-timezone>
    actual_versions: <fault-time-core-luci-and-active-mihomo-identities>
    expected: <predeclared-expectation>
    actual_error: <exact-redacted-error-code-message-and-terminal-status>
    impact: <broken-function-and-blocked-downstream-cases>
    evidence: [<original-terminal>, <logs>, <identity>, <state>]
    diagnosis: <proven-cause-or-explicitly-unresolved>
    status: <open-fixed-pending-retest-verified-on-candidate-or-historical-only>
    fix: {repository: <owning-repo-or-none>, commit: <sha-or-none>}
    retest: <new-candidate-attempt-and-result-or-not-executed>
    workaround: <none-or-authorized-separate-attempt-with-scope-and-result>
error_reconciliation:
  source_inventory: <all-task-probe-tool-and-ui-event-records>
  event_index: <each-error-warning-to-anomaly-defect-or-predeclared-expectation>
  unmapped_event_count: <must-be-zero-for-report-completeness>
  executed_attempts_without_result: <must-be-zero-for-report-completeness>
  completeness: <complete-or-incomplete>
core_results:
  meta: <PASS-FAIL-BLOCKED>
  smart: <PASS-FAIL-BLOCKED>
  meta_to_smart: <PASS-FAIL-BLOCKED>
  smart_to_meta: <PASS-FAIL-BLOCKED>
release_assessment:
  required_case_ids: [<all-required-function-cases>]
  blocking_anomaly_or_defect_ids: [<unresolved-blocking-items-or-empty>]
  nonblocking_observation_ids: [<retained-external-observations-or-empty>]
  rationale: <functional-evidence-completeness-and-scope-limits>
release_decision: <PASS-or-BLOCKED>
reviewed_by: <reviewer>
```

放行者逐項確認：

1. G00–G03、雙核心 A–E（含每個選定舊版配對）、K、V、F、N、R、X、Z 都有
   第 7 節矩陣要求的必要功能結果。Meta、Smart、Meta → Smart、Smart → Meta
   四個必要功能分項全部 PASS 才能放行；不得 N/A 或以其中一項代替另一項。
   案例級 N/A 僅限本文已允許的條件分支，shared 只限明列子項，沒有未解的
   必要功能 FAIL／BLOCKED。外部觀察結果不直接併入這四個分項。
2. 訂閱／配置內容、task、PID、controller、接管與 LAN 請求證據屬於同輪候選；
   不以 CI、封裝結果、舊事故紀錄或後來的手動修復頂替。
3. 大訂閱沒有縮小、預設策略同步未被主線關閉、失敗請求沒有被成功重試抹掉；
   Smart 模型已載入、冷暖態分開，Meta 沒有依賴 Smart 殘留，兩向切換與成對更新
   都有實際程序／配置／流量證據，不只有 UI 選項或磁碟檔名。
4. 發布 tag 必須指向受測 commit。若 Release workflow 重建資產，下載後對照
   已驗收的 hash；不一致不得沿用通過紀錄或繼續廣播，須調查／重驗。
   要求「公開前逐位元相同」時，管線必須先具備候選資產直接提升的能力；
   現行 tag 自動發布不能被文件描述成已具備該能力。
5. 證據脫敏，記錄未覆蓋平台／協定，明確寫「iStoreOS QEMU 驗收」，不擴大成實機結論。
6. 所有已執行功能／子案例、歷史基線及補救嘗試均在索引；非預期失敗都有異常 ID，
   已確認缺陷另有缺陷 ID，沒有把未知外部逾時一律當作 Core／LuCI 待修 Bug。
   預期拒絕／內建重試有事前契約及實際斷言，warning 有判讀。對帳不得有未映射
   事件或缺少結果的已執行嘗試；即使整輪已 FAIL，也要補齊其餘已發現缺陷。
7. 沒有把人工 refresh／保存應用後的成功當成首次初始化通過，沒有把補救副本
   當正常 S2；每個下游的基線資格可追溯。已修復缺陷有新候選的直接重驗，
   原始 FAIL 不刪除；歷史版本缺陷與目前候選結果分列，不互相冒充。
8. 每個阻擋項指出尚未成立的必要斷言；每個非阻擋外部異常有功能驗證證據、
   歸屬信心及未確認範圍。不以「外網一定正常」作放行前提，不要求外部異常
   都有修復 commit；也不以「可能是外網」豁免候選整合故障或缺失的功能證據。

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
