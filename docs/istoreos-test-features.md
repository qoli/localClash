# iStoreOS 功能測試追蹤表

這是持續維護的功能與歷史證據帳本。先找出變更影響的功能，再按
[測試 SOP](istoreos-release-test-sop.md) 決定本輪實測、沿用或補讀證據。
本表不是每輪排程，也不要求每個功能的最後實測版本與本輪候選相同。

## 記錄與更新方式

- 穩定 ID 表示功能契約；舊 A/B/K/F 等代號只索引
  [案例參考](istoreos-test-cases.md)，不表示執行先後或阻擋關係。
- 每個核心各自追蹤。欄內順序是「最後已核對實測 Core／LuCI 配對；結果；
  已覆蓋範圍與證據」。`shared` 只用於未選核心的套件或純靜態 UI 檢查。
- 一列可以列出同契約的 `variant`，例如策略同步開／關、元件種類或故障時機。
  本輪計畫要寫明所選 variant 和斷言；結果不同時在欄內分列，或把穩定且常用的
  子功能拆成新 ID。不能用某一子項的 PASS 表示整列所有變體均通過。
- 實測才更新最後測試版本；`reuse` 只寫入本輪計畫及採納證據，不把舊版 PASS
  改成本版 PASS。版本配對還須透過證據找到 commit 或產物 SHA、Mihomo
  flavor/version/hash、環境、輸入、實際入口與時間。未發布版本可用 commit。
- `PASS`、`FAIL`、`ERROR`、`NOT_RUN` 等 attempt 結果依 SOP 定義。
  **「待核對」是歷史匯入狀態，不是測試結果，也不等於未測或必須重測。**
  先補讀既有紀錄；只有變更影響或必要證據缺口才安排補測。
- 最近實測失敗不能被較舊 PASS 遮蔽；保留兩者及 failure 的處置紀錄。環境
  ERROR 或未執行的 attempt 不假造新的產品測試版本，也不刪除先前產品結果。
  修復後的 PASS 必須連回失敗與修復身分，原始報告不可覆寫。
- V1–V6 的 task、版本、材料、runtime、接管及網路是相關功能的取證要求，
  不另建立六個「產品功能」。同一次操作可證明多個明確斷言，無須為填表重跑。

## 安裝與純靜態介面

| 功能 ID | 功能／可獨立選測斷言 | 核心範圍 | 案例索引 | 最後實測 Core／LuCI；結果；證據 |
| --- | --- | --- | --- | --- |
| PKG-INSTALL | iStore 真正斷 WAN 離線安裝；安裝後為未初始化狀態；同包重裝保留額外檔案、無半套檔案。variant：初裝／重裝 | shared | [G03](istoreos-test-cases.md) | 待核對 |
| PKG-ARCH-REJECT | 不符架構的 bundle 明確拒絕，正式檔案摘要不變 | shared | [G03](istoreos-test-cases.md) | 待核對 |
| PKG-INTEGRITY-REJECT | bundle 內部 checksum 不符時拒絕，正式檔案摘要不變 | shared | [G03](istoreos-test-cases.md) | 待核對 |
| UI-STATIC | 現行頁面、文字、入口、狀態排版與錯誤展示可用；不含 runtime／長任務結果 | shared | [F10](istoreos-test-cases.md) | 待核對 |

## 初始化、更新與資料保留

| 功能 ID | 功能／可獨立選測斷言 | 案例索引 | Meta：最後實測 Core／LuCI；結果；證據 | Smart：最後實測 Core／LuCI；結果；證據 |
| --- | --- | --- | --- | --- |
| INIT-POLICY | S1 從「开始初始化」完成選定核心、訂閱、策略、配置、runtime 與接管。variant：完整預設／minimal；乾淨安裝主線使用完整預設 | [A、F3、V/N](istoreos-test-cases.md) | v0.1.81／0.1.0-76；待核對（[E01](#e01) 有 A/Meta 延續證據，原始 UI 初始化鏈未完整匯入） | v0.1.81／0.1.0-76；待核對（[E02](#e02) 有 A/Smart 延續證據，原始 UI 初始化鏈未完整匯入） |
| UPGRADE-EMPTY | 舊版未配置 S1 先升級 LuCI，再從新版 UI 初始化；不假造既有配置。variant：受支援舊版配對 | [C](istoreos-test-cases.md) | 待核對 | 待核對 |
| UPDATE-REPEAT | 候選 S2 從 UI 一鍵更新並重跑同版本；軟體／材料檢查點可讀回，無降級、重複 runtime 或任務鎖 | [B、V](istoreos-test-cases.md) | v0.1.81／0.1.0-76；待核對（[E03](#e03) 兩次 rpcd/ubus 交易終態 PASS；UI 與完整斷言待核對） | v0.1.81／0.1.0-76；待核對（[E04](#e04) 兩次 rpcd/ubus 交易終態 PASS；有來源 warning，UI 與完整斷言待核對） |
| UPGRADE-CONFIGURED | 舊版已配置 S2 升級候選 LuCI，再由新版 UI 一鍵更新；保留原核心及指定資料。variant：舊版配對／正常或已知故障起點 | [D](istoreos-test-cases.md) | 待核對 | 待核對 |
| UPGRADE-HANDOFF | 從舊版 UI 升級，實際 helper/re-exec、Core/MCP 身分及兩個檢查點正確。variant：支援的直接／兩步升級、舊版配對 | [E](istoreos-test-cases.md) | 待核對 | 待核對 |
| UPDATE-UNINITIALIZED | S1 一鍵更新只處理適用元件；無訂閱時材料明確跳過，不自行初始化／接管，之後可正式初始化 | [A–E 的 S1 更新分支](istoreos-test-cases.md) | 待核對 | 待核對 |
| UPDATE-STOPPED | 已配置且使用者停止 runtime／接管後更新，不擅自重新開啟；明確手動恢復後正常 | [A–E 的 stopped-S2 分支](istoreos-test-cases.md) | 待核對 | 待核對 |
| UPDATE-PRESERVATION | 更新保留訂閱、網站列表／順序、同步偏好、開機意圖、額外檔案與核心選擇；策略補丁依明示契約處理。variant：同步開／關、B/D/E | [資料保留矩陣](istoreos-test-cases.md) | 待核對 | 待核對 |

## 核心行為

| 功能 ID | 功能／可獨立選測斷言 | 案例索引 | Meta：最後實測 Core／LuCI；結果；證據 | Smart：最後實測 Core／LuCI；結果；證據 |
| --- | --- | --- | --- | --- |
| CORE-CONFIG | 所選 flavor 與 profile、binary、PID executable、controller 相符；Meta `url-test`／Smart `smart` 及專用參數符合 intent | [K1、K2、F3](istoreos-test-cases.md) | 待核對 | 待核對 |
| CORE-STARTUP | 首次與正常停止後啟動可完成 runtime／controller／接管及首個 LAN 請求；記錄耗時、CPU/RSS。variant：冷／暖，Smart 模型與統計起點 | [K3](istoreos-test-cases.md) | 待核對 | 待核對 |
| CORE-STATE | 刷新、hot reload、process restart、同核心版本升級後狀態符合持久化／遷移契約；Smart 模型、統計、排名可重新載入 | [K5](istoreos-test-cases.md) | 待核對 | 待核對 |
| CORE-VALIDATION | 活躍 runtime 期間由產品入口使用目標核心隔離驗證；不爭用 live DB、不破壞運行狀態 | [K5、V4](istoreos-test-cases.md) | 待核對 | 待核對 |
| CORE-SELECTION | 兩個可識別節點下自動組實際選路正確；節點失敗與恢復有連線鏈／出口證據。variant：TCP／UDP、健康／故障／恢復 | [K6](istoreos-test-cases.md) | 待核對 | 待核對 |
| CORE-PAIR-UPDATE | Meta/Smart binary 成對更新並保留活躍核心；候選驗證或第二檔替換失敗時按交易恢復，不能半新半舊假成功。variant：成功／Meta 候選失敗／Smart 候選失敗／第二檔替換失敗 | [K7](istoreos-test-cases.md) | 待核對 | 待核對 |

| 功能 ID | Smart 專屬功能／可獨立選測斷言 | 案例索引 | 最後實測 Core／LuCI；結果；證據 |
| --- | --- | --- | --- |
| SMART-MODEL-LOAD | 有效模型有 SHA、正式 runtime 載入及可用證據；檔案存在或配置 `type: smart` 不足以通過 | [K4](istoreos-test-cases.md) | 待核對 |
| SMART-MODEL-FAILURE | 模型缺失、損壞、下載失敗各有可定位終態；恢復來源後從正式入口重新載入。variant 分別記錄 | [K4](istoreos-test-cases.md) | 待核對 |
| SMART-MODEL-UPDATE | 有效模型更新可載入；無效更新候選不得覆蓋有效模型。variant：有效／無效候選 | [K4](istoreos-test-cases.md) | 待核對 |

## 訂閱、網站分流與一般入口

| 功能 ID | 功能／可獨立選測斷言 | 案例索引 | Meta：最後實測 Core／LuCI；結果；證據 | Smart：最後實測 Core／LuCI；結果；證據 |
| --- | --- | --- | --- | --- |
| SUB-EDIT | 多來源及單節點 URI 新增／修改／刪除、保存並應用及刷新後，來源歸屬、合併材料、重新開頁讀回正確 | [F1](istoreos-test-cases.md) | 待核對 | 待核對 |
| SUB-INVALID | 空白／結構非法訂閱明確拒絕，原合法保存狀態不被清空 | [F1](istoreos-test-cases.md) | 待核對 | 待核對 |
| SUB-PARTIAL-FAILURE | 已配置來源抓取／解析及來源 cache 均失敗，但另有有效來源時，failed/warning 可見並跳過，只提交健康來源材料。variant：保存刷新／更新材料階段 | [F1、X2 partial-success](istoreos-test-cases.md) | 待核對 | 待核對 |
| SUB-ALL-FAILURE | 全部來源無效則明確失敗、保留 joined causes 與舊合併材料；不破壞已完成軟體檢查點，修正後重試可成功 | [F1、X2 all-invalid](istoreos-test-cases.md) | 待核對 | 待核對 |
| SUB-LARGE | 完整大樣本超過歷史 240 秒邊界時進度／heartbeat 持續，正常完成；無進度時可觀測且有界處置 | [F2](istoreos-test-cases.md) | 待核對 | 待核對 |
| SITE-ROUTING | 新增／刪除直連與代理域名、子域及 wildcard；同域衝突依最新成功規則，刪除後恢復前項，實際命中與出口正確 | [F4](istoreos-test-cases.md) | 待核對 | 待核對 |
| SITE-INVALID | 非法 pattern 明確拒絕，既有網站分流資料及已載入規則不被破壞 | [F4](istoreos-test-cases.md) | 待核對 | 待核對 |
| RUNTIME-LIFECYCLE | 從現行入口啟動／重啟／停止；狀態及實際 PID／服務相符，停止不留 runtime 或 owned 接管殘留 | [F5](istoreos-test-cases.md) | 待核對 | 待核對 |
| TAKEOVER-INDEPENDENT | runtime 運行時獨立停止／套用接管；UI 分開表達 runtime 與接管，僅處理 localClash owned 資產 | [F5](istoreos-test-cases.md) | 待核對 | 待核對 |
| DASHBOARD-USE | LuCI 打開 Dashboard，認證／資源／API 可用；專用測試組選擇生效，實際請求及出口符合選擇 | [F6](istoreos-test-cases.md) | 待核對 | 待核對 |
| MCP-CONNECT | 從頁面接入資訊完成 initialize、tools/list、environment_inspect 與路由唯讀查詢，guest 身分與 LuCI/controller 一致 | [F7](istoreos-test-cases.md) | 待核對 | 待核對 |
| DNS-OPTIMIZATION | dnsqualify 狀態與量測如實顯示；合格環境套用及明確重啟後正常，刪除回加密 DNS 基線，LAN 名稱仍可用 | [F8](istoreos-test-cases.md) | 待核對 | 待核對 |
| DNS-IDENTITY-REJECT | 量測期間 WAN device identity 改變時明確拒絕提交，保留既有有效配置 | [F8](istoreos-test-cases.md) | 待核對 | 待核對 |
| DNS-HEALTH-LEASE | DNS 接管初始保留 WAN/dnsmasq 基線，TCP/UDP 探測健康才續租；探測失敗或 guard／Mihomo 退出後租約自行到期恢復基線，舊 generation／已停止接管不得再續租。variant：健康／探測失敗／程序退出／generation 更換／明確停止 | [F5、N3、R4/R5；現行 DNS guard 契約](istoreos-test-cases.md) | 待核對（既有報告有 lease_active；未匯入失效／自行恢復實測） | 待核對（既有報告有 lease_active；未匯入失效／自行恢復實測） |
| COMPONENT-MAINTENANCE | 從可見維護入口更新並核對實際版本／SHA，之後仍可初始化／更新。variant：LuCI／Core／Mihomo／Dashboard | [F9](istoreos-test-cases.md) | 待核對 | 待核對 |
| MCP-SERVICE | MCP 服務停止／啟動與實際 procd、listener、協定連線一致，無孤兒服務或假健康 | [F9](istoreos-test-cases.md) | 待核對 | 待核對 |
| UI-TASK | 長任務日誌、互斥寫入／重複點擊、重新載入後找回原任務及終態，無無限 busy 或未處理錯誤 | [F10](istoreos-test-cases.md) | 待核對 | 待核對 |

## 網路功能

| 功能 ID | 功能／可獨立選測斷言 | 案例索引 | Meta：最後實測 Core／LuCI；結果；證據 | Smart：最後實測 Core／LuCI；結果；證據 |
| --- | --- | --- | --- | --- |
| NET-DIRECT | 真實 LAN client 經受測轉送路徑到受控 DIRECT 端點，身分及 payload 正確。variant：HTTP／TCP echo | [N1](istoreos-test-cases.md) | v0.1.81／0.1.0-76；待核對（[E03](#e03) 更新後 HTTP 3 次 200／72 bytes，完整路徑與 payload 斷言待核對） | v0.1.81／0.1.0-76；待核對（[E04](#e04) 第二次更新後 HTTP 3 次 72 bytes，完整路徑與 payload 斷言待核對） |
| NET-PROXY | 真實 LAN client 由 localClash 規則選到可識別代理鏈，代理／端點能辨別出口；不以顯式 HTTP_PROXY 直連代理代替透明接管 | [N2](istoreos-test-cases.md) | v0.1.81／0.1.0-76；待核對（[E03](#e03) 顯式 CONNECT A/B 子項通過；透明策略選路證據待核對） | v0.1.81／0.1.0-76；待核對（[E04](#e04) 報告有 CONNECT A/B 子項；透明策略選路證據待核對） |
| NET-DNS-LAN | router／受控 DNS 查詢與答案、正向本地域名／DHCP 名稱與 LAN 服務符合設定；接管與 local bypass 分別正確 | [N3](istoreos-test-cases.md) | v0.1.81／0.1.0-76；待核對（[E03](#e03) 有公網解析、未宣告名稱 NXDOMAIN；正向本地解析／bypass 待核對） | v0.1.81／0.1.0-76；待核對（[E04](#e04) 有公網解析與 NXDOMAIN；正向本地解析／bypass 待核對） |
| NET-UDP-DIRECT | IPv4 LAN client 向受控 WAN UDP 端點發唯一 payload，應用層回應完全符合預先指定格式與 payload，路由/TUN、出入封包及 DIRECT 意圖相符 | [N4 DIRECT](istoreos-test-cases.md) | v0.1.81／0.1.0-76；PASS（2026-09-08，更新後 DIRECT echo；[E03](#e03)，controller 保存為計數，非完整 connection object） | v0.1.81／0.1.0-76；PASS（2026-09-08，第二次更新後 DIRECT echo；[E04](#e04)，controller 保存為計數，非完整 connection object） |
| NET-UDP-PROXY | 具 UDP 代理能力的節點下，唯一 LAN UDP payload 經預期代理鏈並有應用層回應；不沿用 DIRECT 或 TCP 結果 | [N4 代理意圖、K6](istoreos-test-cases.md) | 待核對 | 待核對 |
| NET-IPV6 | 宣稱支援且有已驗證 IPv6 路徑時，直連／代理／DNS／UDP 的捕獲與出口正確。variant 明列協定；不適用須附環境／契約理由 | [N5](istoreos-test-cases.md) | 待核對 | 待核對 |
| NET-CONTINUITY | 初始化、兩個更新檢查點、重啟／停止／恢復期間持續探測；準備不提前破壞舊鏈、切換後無持續黑洞，中斷有量測 | [N6](istoreos-test-cases.md) | 待核對 | 待核對 |

## 開機、事件與恢復

| 功能 ID | 功能／可獨立選測斷言 | 案例索引 | Meta：最後實測 Core／LuCI；結果；證據 | Smart：最後實測 Core／LuCI；結果；證據 |
| --- | --- | --- | --- | --- |
| RESTORE-BOOT | UI 開機恢復意圖在真正 reboot 後生效，boot ID 改變；關閉時不自啟，手動啟動可恢復。variant：開／關 | [R1、R2](istoreos-test-cases.md) | 待核對 | 待核對 |
| RESTORE-WAN | 測試 guest WAN ifdown/ifup／ifupdate 後按 same-boot 接管意圖恢復，真實網路可用 | [R3](istoreos-test-cases.md) | 待核對 | 待核對 |
| RESTORE-EXPLICIT-STOP | 使用者明確停止接管後，WAN 事件與背景恢復窗口不能擅自重開；手動恢復成功 | [R4](istoreos-test-cases.md) | 待核對 | 待核對 |
| RESTORE-CRASH | managed 程序非預期退出後 watchdog／任務／接管狀態如實；支援恢復路徑成功。variant：空閒／更新期間 | [R5](istoreos-test-cases.md) | 待核對 | 待核對 |
| RESTORE-REPEAT | 三輪 runtime／接管開始、重啟、停止、恢復後無規則、路由、PID、鎖、暫存資產累積，最後網路正常 | [R6](istoreos-test-cases.md) | 待核對 | 待核對 |

## 故障、取消與重置

| 功能 ID | 功能／可獨立選測斷言 | 案例索引 | Meta：最後實測 Core／LuCI；結果；證據 | Smart：最後實測 Core／LuCI；結果；證據 |
| --- | --- | --- | --- | --- |
| FAULT-DOWNLOAD-RETRY | 來源短暫失聯按既有次數／逾時恢復；持續失聯明確失敗，恢復來源後正常更新。variant：短暫／持續 | [X1](istoreos-test-cases.md) | 待核對 | 待核對 |
| FAULT-DOWNLOAD-INTEGRITY | 空檔或 checksum 錯誤不得安裝、不得假成功；保留既有有效檔案，修正來源後重試成功。variant：空檔／checksum | [X1](istoreos-test-cases.md) | 待核對 | 待核對 |
| FAULT-CONFIG-REJECT | 非法材料由目標核心驗證拒絕，不提交半套配置；前後材料摘要／載入狀態可核對，修正後成功 | [X3 配置驗證](istoreos-test-cases.md) | 待核對 | 待核對 |
| FAULT-RELOAD | 驗證後 controller 不可達時 hot reload 明確失敗，不假報已載入；合法檢查點及恢復重試正確 | [X3 熱載入](istoreos-test-cases.md) | 待核對 | 待核對 |
| FAULT-INSTALL-START | 資源不足／listener 占用等安裝或啟動失敗可定位，不盲目重啟；解除注入後正式修復路徑成功。variant：安裝／啟動 | [X4](istoreos-test-cases.md) | 待核對 | 待核對 |
| TASK-CANCEL | 初始化／一鍵更新的準備／材料階段取消，有終態、釋放鎖、停止子程序，合法檢查點保留，無延遲 worker 覆寫；再啟可成功 | [X5](istoreos-test-cases.md) | 待核對 | 待核對 |
| TASK-INTERRUPTION | UI/RPC 連線中斷或套件 re-exec 後仍可找回同一任務終態，不重複交易；helper/MCP 身分正確 | [X6](istoreos-test-cases.md) | 待核對 | 待核對 |
| UPGRADE-POLICY-REJECT | 支援遷移範圍內的已移除／不相容舊策略，在關同步更新時明確拒絕且資料可恢復；開同步後升級成功 | [X7](istoreos-test-cases.md) | 待核對 | 待核對 |
| RESET-WORKSPACE | 先停止 runtime／接管，再由完整 workspace reset 確認範圍並刪除；不接受任意路徑、不刪 workspace 外的 Core／LuCI，不留 owned 接管 | [Z](istoreos-test-cases.md) | 待核對 | 待核對 |
| RESET-REINITIALIZE | reset 後 UI 真實未初始化，可按原核心重新初始化並恢復網路；Smart 應刪除的模型／統計以冷態重驗 | [Z、K3/K4](istoreos-test-cases.md) | 待核對 | 待核對 |

## 首次匯入的證據範圍（2026-09-08）

本次只讀取少量既有報告及所列原始證據，沒有啟動 VM、重跑功能或改寫原始報告
結論。尚未匯入的其他功能全部標「待核對」，不能解讀為過去從未執行。
表中的最後版本是**目前已核對到的最近實測**；不保證其他歷史目錄沒有較新紀錄。
採納前仍需檢查同功能、核心、variant 是否存在較新的失敗或不適用變更。

以下共同配對是 Core `v0.1.81`／LuCI `0.1.0-76`。報告候選標記
`f2832cc9e2ecadeacdcc6718e343c704cd7235ac` 不當作兩個 repository 的共同 commit；
Core、LuCI commit 分別仍待核對。已讀到的具體產物身分如下，僅界定歷史證據，
不宣稱它們就是目前工作樹或最新發布版本：

| 產物 | 已核對身分 |
| --- | --- |
| Core linux-amd64 | `v0.1.81`；SHA `6587cd7e24d1dc3953eef4a76076b9541f8a2c2be5aed1f3e5f7c776f5d1379a` |
| LuCI bundle 內 rpcd helper | `0.1.0-76`；SHA `e08afc6722578fb1c2294d4c98d59790937ebb64e07fb7a93b29db6d0b78d9b5` |
| Meta | `v1.19.30`；SHA `20ba567571d9ca642bedecbb01f8092cab0f1679100087ef1a4a2efac0ed5494` |
| Smart | `alpha-smart-651ca46`；SHA `707ec1ce441c62d8083d8b5e147a5f56d78e1b4b9717db6015f8edc6d0d1acdc` |

所有原始證據位於忽略追蹤的本機 `.runtime/`。下列連結只提供定位，不把 runtime
檔案加入 Git；其他使用者取得不到時應記證據可用性缺口，不能默認不存在或通過。
分享報告須移除秘密並提供可存取的受控證據副本。

### E01

2026-09-07 [A/Meta attempt-07 報告](../.runtime/istoreos-acceptance/20260907-release-v076/execution/formal-a-e-attempt-07-meta-recovery/formal-a-e-attempt-07-meta-recovery-report.md)
記錄承接 attempt-06 的初始化狀態，透過 `runtime_start_takeover` 完成延續取證。
本次已讀報告中的 Core/LuCI、Meta hash、task、takeover、N1–N4 範圍；未完整
補讀最初 UI 初始化的跨 attempt 原始鏈。原 A/Meta PASS 報告保留，功能表先記部分證據。

### E02

2026-09-08 [A/Smart attempt-21 報告](../.runtime/istoreos-acceptance/20260907-release-v076/execution/formal-a-e-attempt-21-smart-promotion/formal-a-e-attempt-21-smart-promotion-report.md)
組合 attempt-11 父本、attempt-20 UDP 前置及 attempt-21 運行時取證。
本次已讀報告的實際 `runtime_start_takeover`、LuCI/helper/Smart 身分與網路範圍；
未完整匯入最初初始化 UI、模型載入與所有父本原始證據。原 A/Smart PASS 報告保留。

### E03

2026-09-08 [B/Meta attempt-27 報告](../.runtime/istoreos-acceptance/20260907-release-v076/execution/formal-a-e-attempt-27-bmeta-mirror/formal-a-e-attempt-27-bmeta-mirror-report.md)
及 [attempt-28 延續報告](../.runtime/istoreos-acceptance/20260907-release-v076/execution/formal-a-e-attempt-28-bmeta-vn/formal-a-e-attempt-28-bmeta-vn-report.md)。
27 的更新交易成功但 LAN client 未就緒；28 在同 hash 父本補網路證據，未重跑更新。
本次讀到以下原始內容：

- [第二次更新終態](../.runtime/istoreos-acceptance/20260907-release-v076/execution/formal-a-e-attempt-27-bmeta-mirror/raw/oneclick2-final-task.log)：`done=true`、`exit_code=0`，實際為 rpcd/ubus 入口。
- [runtime／元件身分](../.runtime/istoreos-acceptance/20260907-release-v076/execution/formal-a-e-attempt-28-bmeta-vn/raw/router-rpcd-final-identity.log)：LuCI `0.1.0-76`、Meta 版本及兩份 binary/helper hash。
- [N1–N3 原始 client 操作](../.runtime/istoreos-acceptance/20260907-release-v076/execution/formal-a-e-attempt-28-bmeta-vn/raw/client-vn-n1-n3.log)：HTTP 72-byte 回應；N2 使用明確 `HTTPS_PROXY` 直連受控 CONNECT proxy。該子項不能自行證明透明策略選路。報告中的公網解析／NXDOMAIN 也不證明正向本地名稱解析。
- [N4 client exact echo](../.runtime/istoreos-acceptance/20260907-release-v076/execution/formal-a-e-attempt-28-bmeta-vn/raw/client-n4-active.log)：三個不同 payload 均有 probe 判定的預期回應；[controller 計數](../.runtime/istoreos-acceptance/20260907-release-v076/execution/formal-a-e-attempt-28-bmeta-vn/raw/controller-n4-loop-read.log) 有目的地、來源、Tun、DIRECT 與 port 的計數。本次亦讀取 `raw/router-n4-final.log`、`raw/endpoint-n4-final.log`，核對 utun/eth2/endpoint 的 request/reply 及 client 回程；僅對已記錄 IPv4 DIRECT echo 採納，不外推 UDP 代理或其他生命週期。

27 的環境缺口及 28 的 DNS/input、endpoint 路由補正仍保留；沿用時要一併考慮
這些測試拓撲條件。表格不改寫原報告的 B/Meta 判定。

### E04

2026-09-08 [B/Smart attempt-29 報告](../.runtime/istoreos-acceptance/20260907-release-v076/execution/formal-a-e-attempt-29-bsmart-vn/formal-a-e-attempt-29-bsmart-vn-report.md)。
兩次 rpcd/ubus 更新成功，第二次更新後仍為 Smart，報告保留第一輪 UDP route
缺漏與訂閱來源 404／cache 使用警告。本次讀到：

- [第二次更新 task 終態](../.runtime/istoreos-acceptance/20260907-release-v076/execution/formal-a-e-attempt-29-bsmart-vn/raw/oneclick2-task-final.log)：`done=true`、`exit_code=0`、Smart 版本、warnings，不能把終態成功擴成所有訂閱錯誤分支通過。
- [最終 hash 及實際來源](../.runtime/istoreos-acceptance/20260907-release-v076/execution/formal-a-e-attempt-29-bsmart-vn/raw/final-hashes-env-procs.log) 與 [Core manifest](../.runtime/istoreos-acceptance/20260907-release-v076/execution/formal-a-e-attempt-29-bsmart-vn/fixture/endpoint-root/candidate/core-v0.1.81-manifest-endpoint.json)：對應上述版本／產物身分。
- [第二次 N1](../.runtime/istoreos-acceptance/20260907-release-v076/execution/formal-a-e-attempt-29-bsmart-vn/raw/oneclick2-n1-final.log)：三個 72-byte 回應；[第二次 N3](../.runtime/istoreos-acceptance/20260907-release-v076/execution/formal-a-e-attempt-29-bsmart-vn/raw/oneclick2-n3-final.log)：公網名稱解析成功、未宣告名稱 NXDOMAIN。完整 N1/N2/N3 功能斷言仍待核對。
- [第二次 N4 client](../.runtime/istoreos-acceptance/20260907-release-v076/execution/formal-a-e-attempt-29-bsmart-vn/raw/oneclick2-n4-client.log)：三個不同 payload 均有 probe 判定的預期回應；[controller 計數](../.runtime/istoreos-acceptance/20260907-release-v076/execution/formal-a-e-attempt-29-bsmart-vn/raw/oneclick2-controller-loop-read.log) 有目的地、來源、Tun、DIRECT、port 計數。本次亦讀取 `raw/oneclick2-n4-router-final2.log`、`raw/oneclick2-n4-endpoint-final.log`，核對 router/endpoint 雙向封包及 client 回程。採納範圍限 IPv4 DIRECT UDP echo；echo 協定回應含格式前綴，不宣稱整個 UDP 回應與請求長度相同。

## 功能邊界與尚待核對項

現行 LuCI `overview.js` 的 `bootstrap_default`、`one_click_update`、runtime／
takeover 操作，`subscription.js` 的 `subscription_setup_async`，以及維護頁
`index.js` 的元件、服務、dnsqualify、`reset` RPC 是本表入口依據。
案例執行仍須核對受測版本的真實 UI；目前不新增健康 S2 運行中跨核心切換、
配置單獨 reset、DNS 資格到期自動處理等不存在的 UI 功能。

DNS health lease 是已落地的獨立接管能力：本次唯讀核對 LuCI 的 `dns-guard`、
`takeover`／`takeover-apply` 與 `scripts/test-dns-health-lease.sh`。它使用 nft
`flags timeout` 集合及 WAN/dnsmasq 基線，與 DNS 最佳化資格到期是不同契約。
來源碼及 mock 測試內容只用來補齊功能定義，不當作本輪 QEMU 實測 PASS。

首次匯入尚未核對的內容包括：其餘功能的歷史結果、Core/LuCI 各自 commit、
所有已選舊版配對／同步策略變體、最初 A 路徑 UI 原始證據、更新的 UI 操作證據、
透明代理規則命中、正向本地名稱／LAN bypass、UDP 代理及 IPv6、不同操作期間
連續性，以及完整歷史範圍內是否有更晚的失敗。這些是資料待核對範圍，
不是本次文件重寫所新增的全表重測排程。
