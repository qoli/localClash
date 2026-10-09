# iStoreOS 功能測試表

本表枚舉 **LocalClash 自己實作的完整產品能力**，用於按變更選測及追蹤最後實測版本。
新增、修改、刪除、保存、套用是功能內的操作，不各建一列；CLI、MCP、LuCI 是入口，
不因入口不同重複計算功能。測試目的是發布前找出功能串接問題，完成必要驗證與修復，不要求每次重跑全表。

## 功能導航

| 找什麼 | 分區 | 功能 ID |
| --- | --- | --- |
| 來源、刷新、節點 | [訂閱管理](#subscriptions) | SUB-* |
| 網站、規則包、群組 | [網站與路由設定](#sites) | SITE-*、RULE-*、PROXY-*、POLICY-* |
| 模板、補丁、生成與套用 | [配置管理](#config) | CONFIG-* |
| 核心選用、啟停 | [核心管理](#cores) | CORE-*、RUNTIME-* |
| 核心及資源更新 | [元件更新](#updates) | COMPONENT-* |
| 狀態、診斷、MCP | [狀態與診斷](#access) | STATUS-*、DIAG-*、MCP-* |
| 初始化、資料與重置 | [工作區管理](#workspace) | WORKSPACE-* |
| OpenWrt 安裝、任務與接管 | [LuCI 整合能力](#luci) | LUCI-* |

<a id="scope"></a>
## 責任與選測範圍

- **Core**：LocalClash 的資料、規則、配置及核心管理能力。對 Mihomo 保證正確核心、
  配置生成與交付、驗證及正常啟動；不測其模型、選路演算法、協定實作或效能。
- **LuCI**：另列 OpenWrt 包裝、UI 任務、更新編排及網路接管。
  不窮舉所有傳輸協定與環境排列，但功能契約宣稱的 TCP／UDP／DNS 資料面必須由
  獨立 client 實測，受控請求或內部狀態不能代替。
- **共用**：選代表核心驗功能的完整操作。需要另一核心時，只補受影響配置／啟動介面；
  不為每項預設 Meta／Smart 兩份結果。配置差異是 CONFIG-RENDER 的測試情境。
- **按核心差異**：變更涉及 flavor、binary、配置驗證／啟動參數時，驗相應核心；
  兩邊都受影響才測兩邊。表中實際測過哪個核心仍如實記錄。

## 記錄規則

最後版本依序為 **localClash Core／LuCI**；CLI／MCP 測試不涉及 LuCI 時記不適用。
連結證據保留實際入口、核心版本／SHA、環境、時間、操作及結果。只驗了某個操作，
就記具體範圍，不把局部成功擴成整項能力 PASS。沿用不更新最後實測版本。
「待核對」表示歷史尚未完成匯入，不代表從未測試或必須立即重測。

發現問題後修復並回驗即可；不要求每個 Bug 新增功能、建立認領任務或專用追蹤制度。
自訂域名無法套用，應在「自訂網站分流」正常功能驗證中發現。
下列內容是功能的驗證範圍；實際操作前核對受測版本的正式入口與契約。

<a id="subscriptions"></a>
## 訂閱管理

| 功能 ID | 功能／驗證內容 | 責任／核心範圍 | 最後實測版本；結果；證據 |
| --- | --- | --- | --- |
| SUB-MANAGEMENT | **訂閱來源管理**：設定、修改及移除來源後能重新讀取；支援訂閱 URL 與節點 URI，非法／空白輸入按入口契約拒絕，原有效來源不被誤清空。[實作入口](../product_cli.go) | Core／共用 | v0.1.83／不適用；PASS；URL／節點 URI 增改刪、空白／FTP／空集合拒絕及原設定保留 [E05](#e05) |
| SUB-REFRESH | **訂閱取得與刷新**：取得並解析多來源、合併節點及重建已配置的服務能力篩選材料（如 ChatGPT 可用節點）；來源失敗如實呈現，有有效來源與全部無效的結果不同；失敗保留合法材料。保存來源與刷新生效分別讀回。[實作入口](../product_cli.go) | Core／共用 | v0.1.87／不適用；PARTIAL；同一 qcow2 由正式 v0.1.85 建立並使用 workspace，經 v0.1.86 真實失敗後原狀升級 v0.1.87；正式刷新自動重建 template-owned patches、保留 user state，50 個候選完成雙探測；E12 的獨立負向 oracle 缺口仍保留 [E12](#e12) [E14](#e14) |
| SUB-NODES | **節點查詢**：列出與搜尋已取得節點，名稱／類型與來源一致，不洩漏連線憑證；查詢結果不冒充節點品質或出口地理驗證。[實作入口](../internal/mcp/registry.go) | Core／共用 | v0.1.83／不適用；PASS；MCP 列出／搜尋、來源欄位及敏感資料界線 [E05](#e05) |

<a id="sites"></a>
## 網站與路由設定

| 功能 ID | 功能／驗證內容 | 責任／核心範圍 | 最後實測版本；結果；證據 |
| --- | --- | --- | --- |
| SITE-ROUTING | **自訂網站分流**：新增、刪除普通域名與萬用字元，設定直連／代理並重新開頁；保存、生成規則、熱載入及 controller 讀回一致；合法 DomainSuffix 不被誤判回滾。另驗衝突順序及非法輸入不破壞既有設定。[實作入口](../internal/customsitesapply/transaction.go) | Core；LuCI 提供頁面／共用 | v0.1.97／0.1.0-76；PARTIAL；MCP 普通／萬用字元增刪、active hot reload／controller 讀回、newest-first、非法輸入保留、停止狀態 pending-next-start 及模板同步保留通過；獨立 LAN client 資料面 NOT_RUN [E27](#e27)；LuCI UI 歷史範圍見 [E05](#e05) |
| RULE-PACKS | **規則包查詢與材料取得**：搜尋目錄、查看指定規則包、預取及查詢其規則；來源／類型／快取缺口如實顯示，目錄建議出口不當成已啟用設定。[實作入口](../internal/mcp/registry.go) | Core／共用 | v0.1.83／不適用；PASS；目錄搜尋／查看／預取／讀取／查詢、來源型別、快取缺口及建議出口界線 [E05](#e05) |
| RULE-CUSTOM | **自訂規則與外部規則來源**：建立域名、CIDR、GEOIP 規則及外部 rule-provider 設定，經配置補丁保存並生成；拒絕非法值或無效引用，順序與目標正確。[實作入口](../internal/mcp/registry.go) | Core／共用 | v0.1.83／不適用；PASS；Domain／CIDR／GEOIP／provider 生成、順序與目標、非法值／引用拒絕 [E05](#e05) |
| PROXY-GROUPS | **代理群組設定**：由精確節點或 selector 建立群組，經補丁保存／修改／移除；查詢解析結果及生成成員正確，builder 預覽不冒充已保存或已載入。[實作入口](../internal/mcp/registry.go) | Core／共用 | v0.1.83／不適用；PASS；精確節點／selector builder、保存／修改／移除、解析成員及預覽界線 [E05](#e05) |
| POLICY-GROUPS | **業務策略群組設定**：把網站／應用／規則包對應到現有出口，經補丁保存及生成；引用與優先順序正確，不意外改動其他群組；不驗 Mihomo 自動選路品質。[實作入口](../internal/mcp/registry.go) | Core／共用 | v0.1.83／不適用；PASS；網站／應用／規則包對應、引用／順序及其他群組保留 [E05](#e05) |

<a id="config"></a>
## 配置管理

| 功能 ID | 功能／驗證內容 | 責任／核心範圍 | 最後實測版本；結果；證據 |
| --- | --- | --- | --- |
| CONFIG-TEMPLATE | **策略模板設定**：選取完整預設或 minimal 模板、配置 profile，產生相應補丁及 intent；重設模板只依明示選項處理既有補丁，不默默覆蓋自訂設定。[實作入口](../product_cli.go) | Core／共用 | v0.1.97／不適用；PARTIAL；同一 qcow2 從 v0.1.96 正式 workspace 升級，完整預設模板同步加入 DegYax 並保留 user-owned custom-site 狀態；minimal 未重跑 [E27](#e27)；其他歷史範圍見 [E15](#e15) [E20](#e20) |
| CONFIG-PATCHES | **配置補丁管理**：讀取、預覽、套用、移除、啟停及排序補丁；預覽不落盤，套用後 registry／intent 一致，失效草稿或非法引用明確拒絕。[實作入口](../product_cli.go) | Core／共用 | v0.1.83／不適用；PASS；預覽／套用／移除／啟停／排序／tombstone、registry 恢復及非法／過期草稿拒絕 [E05](#e05) |
| CONFIG-RENDER | **Mihomo 配置生成**：由訂閱、模板、補丁與 profile 生成正確配置。Meta 自動組為 url-test，Smart 為 smart 並移除 tolerance；Smart 參數、群組 priority 與 defaults 按 intent 傳遞且不覆蓋既有值；生成不等於已載入。[實作入口](../product_cli.go) | Core／按核心差異 | v0.1.97／不適用；PASS（Meta／預設模板影響範圍）；同一 qcow2 由公開 v0.1.96 建立 3-source／107-proxy workspace，候選生成 65 rules，DegYax 位於 `GEOSITE,cn,DIRECT` 前，自訂網站維持 newest-first [E27](#e27)；其他核心差異歷史範圍見 [E16](#e16) [E21](#e21) |
| CONFIG-VALIDATION | **配置驗證**：用所選核心驗證生成配置，記錄對應 hash；非法配置不取得通過證明；驗證與活躍程序隔離，不爭用工作目錄。[實作入口](../product_cli.go) | Core／按核心差異 | v0.1.97／不適用；PASS（Meta）；iStoreOS Meta v1.19.32 正式 isolated config-test 對候選配置 SHA `8da8d8bf…3b8df` 通過並記錄 attestation [E27](#e27)；其他核心差異歷史範圍見 [E16](#e16) [E21](#e21) |
| CONFIG-APPLY | **配置套用**：按所選入口核對候選檔提交或 runtime 載入；config-promote 是其中一個檔案提交入口，不是所有流程的前置。驗證 hash、實際載入規則／組及失敗結果符合契約，提交檔案不當成已熱載入，生成檔存在不當成已生效。[實作入口](../product_cli.go) | Core／按核心差異 | v0.1.97／不適用；PASS（本輪變更範圍）；正式 runtime start、active custom-site hot reload／語義讀回、停止狀態 pending-next-start 及 controller rules 讀回通過 [E27](#e27)；其他歷史範圍見 [E15](#e15) |

<a id="cores"></a>
## 核心管理

| 功能 ID | 功能／驗證內容 | 責任／核心範圍 | 最後實測版本；結果；證據 |
| --- | --- | --- | --- |
| CORE-PROFILE | **核心與運行 profile 設定**：選用正確 flavor、binary、profile 及啟動參數；設定與實際程序／controller 身分一致。LuCI 的核心選擇由初始化入口提供，不虛構運行中切換按鈕。[實作入口](../product_cli.go) | Core／按核心差異 | v0.1.83／0.1.0-76；PASS；Meta／Smart profile、binary／命令列／controller 身分及 LuCI 初始化選擇 [E05](#e05) |
| RUNTIME-MANAGEMENT | **核心生命週期管理**：啟動、停止、程序重啟及必要的重複操作結果正確；配置路徑、workdir、PID／controller 與狀態一致，啟動失敗有終態，不影響非受管程序。[實作入口](../product_cli.go) | Core／按核心差異 | v0.1.83／不適用；PASS；Meta／Smart 啟停／重啟／重複操作、PID／controller、失敗終態及非受管程序保留 [E05](#e05) |

<a id="updates"></a>
## 元件更新

| 功能 ID | 功能／驗證內容 | 責任／核心範圍 | 最後實測版本；結果；證據 |
| --- | --- | --- | --- |
| COMPONENT-MIHOMO | **Mihomo 核心取得與更新**：依平台／架構取得選定來源的兩份核心，核對版本／SHA；成對更新與失敗恢復一致，preflight 使用活躍核心，更新後保留選擇並正常啟動。[實作入口](../product_mihomo_update.go) | Core／共用交易，按核心差異驗啟動 | Core `f23554d`／LuCI `a25a3cc`；PASS（本輪影響範圍，Meta active）；Meta／Smart 成對 SHA、changed／identical pair、現有核心保留、缺核心失敗及單一最終 runtime commit [E18](#e18)；其餘契約沿用 [E05](#e05) |
| COMPONENT-ASSETS | **基礎資源更新**：取得並安裝基礎資源，版本／完整性正確；更新失敗不假報完成，保留正在使用的資料檔映射，之後配置生成可使用實際安裝的資源。[實作入口](../product_cli.go) | Core／共用 | 2026-10-02 更新至 Core v0.1.93／LuCI 0.1.0-83；FAIL；運行中 Smart 讀取 ASN.mmdb 時 SIGBUS，準備階段原地截斷資料檔；修正候選的主機回歸已通過，iStoreOS 功能回驗 NOT_RUN [E19](#e19)；歷史安裝範圍見 [E05](#e05) |
| COMPONENT-DASHBOARD | **Dashboard 資源管理**：取得、更新及提供面板資源，檔案／入口與 controller 連接設定一致；能開啟面板，不驗 Dashboard 自身全部功能。[實作入口](../product_cli.go) | Core；LuCI 提供連結／共用 | Core `11ee263`／LuCI `2ac337f`；PASS；有效 archive 更新、component status、`external-ui`、controller `/ui/`，以及 5 次失敗後 optional skip／原 hash 保留 [E07](#e07) |

<a id="access"></a>
## 狀態與診斷

| 功能 ID | 功能／驗證內容 | 責任／核心範圍 | 最後實測版本；結果；證據 |
| --- | --- | --- | --- |
| STATUS-INSPECT | **產品狀態查詢**：查配置、元件、訂閱及 runtime facts，未初始化／停止／運行／錯誤狀態與實際資料一致；唯讀不改狀態，版本化 facts 不把宿主接管當 Core 所有。[實作入口](../internal/mcp/registry.go) | Core／共用 | v0.1.83／不適用；PASS；未初始化／停止／運行／錯誤、元件／訂閱／runtime facts、唯讀 hash 與接管責任界線 [E05](#e05) |
| DIAG-ROUTING | **路由設定解釋**：按域名、服務或出口查詢編譯 intent 的規則、群組及來源；能對照自訂網站及補丁變更，不把 intent 解釋當活躍流量證據。[實作入口](../internal/mcp/registry.go) | Core／共用 | v0.1.83／不適用；PASS；domain／service／exit 查詢、自訂規則／patch provenance 及 intent／流量證據界線 [E05](#e05) |
| DIAG-HEALTH | **診斷與日誌取得**：執行 doctor／環境檢查、收集產品日誌及讀取受限 controller 日誌／連線；診斷可定位問題、讀取有界且不洩漏秘密，不由缺少連線推論未來路由。[實作入口](../internal/mcp/registry.go) | Core／共用 | v0.1.83／不適用；PASS；doctor、受限日誌／controller／connections、redaction 與空連線推論界線 [E05](#e05) |
| MCP-SERVICE | **MCP 服務與工具存取**：連接正確服務並完成協定初始化、工具發現與呼叫；工具結果／錯誤／權限符合入口契約。檔案讀改與 controller 存取受限定，不把 MCP 當任意路徑或任意 URL 代理。[實作入口](../internal/mcp/registry.go) | Core；LuCI 管理 procd／共用 | v0.1.97／0.1.0-76；PASS（本輪變更範圍）；實際 MCP initialize／tools/list、`custom_sites_list`／`custom_sites_transact`、結構化錯誤及服務停止敏感度／恢復通過 [E27](#e27)；完整安全矩陣歷史範圍見 [E05](#e05) |

<a id="workspace"></a>
## 工作區管理

| 功能 ID | 功能／驗證內容 | 責任／核心範圍 | 最後實測版本；結果；證據 |
| --- | --- | --- | --- |
| WORKSPACE-INIT | **工作區初始化**：由配置／apply 正式入口建立所需來源、profile、策略及材料；已有狀態按宣告操作，不擅自清空。LuCI 引導與接管編排另列 LUCI-INIT。[實作入口](../product_cli.go) | Core／共用，涉及核心時按差異 | v0.1.83／0.1.0-76；PASS；normal／full reset 後由空工作區重建 assets／subscription／profile／config-test，既有狀態不誤清 [E05](#e05) |
| WORKSPACE-RESET | **工作區重置**：預覽及正式重置的範圍一致，普通／完整重置依契約清理；不刪除範圍外檔案或非受管程序，之後可重新初始化。[實作入口](../product_cli.go) | Core／共用 | Core `11ee263`／LuCI `2ac337f`；PASS；normal／full preview 與 execute、範圍外資料／非受管程序保留、重建，以及 Arc checkbox／真 RPC／結果 modal [E07](#e07) |

<a id="luci"></a>
## LuCI 整合能力（另列歸屬）

| 功能 ID | 功能／驗證內容 | 責任／核心範圍 | 最後實測版本；結果；證據 |
| --- | --- | --- | --- |
| LUCI-INSTALL | **OpenWrt 安裝與版本管理**：離線包安裝、重裝、LuCI／Core 安裝更新及支援的舊版交接正常；架構／完整性錯誤拒絕，應保留資料不丟失。Core 自我更新未實作，實際由 helper 管理。[實作入口](../../localclash-luci/openwrt/luci-app-localclash/root/usr/libexec/rpcd/localclash) | LuCI／共用 | Core `591c357`／LuCI v0.1.0-81 候選；PASS（影響範圍）；候選 IPK 在 inactive task 同步 rpcd reload、active task 只寫延後標記，兩路均安裝成功且 rpcd／uhttpd PID 不變 [E17](#e17) |
| LUCI-INIT | **初始化引導**：由頁面提供訂閱、模板及核心，完成 Core 呼叫、配置、啟動及接管；重新開頁顯示真實狀態，失敗可定位。[實作入口](../../localclash-luci/openwrt/luci-app-localclash/root/usr/libexec/rpcd/localclash) | LuCI 編排＋Core／按影響 | v0.1.83／0.1.0-76；PASS；空工作區 Meta／Smart 初始化、訂閱／模板／Core、UI 啟動並接管、重開讀回及可定位失敗後恢復 [E05](#e05) |
| LUCI-UPDATE | **一鍵更新與資料保留**：從頁面更新，兩個檢查點、來源版本及結果可讀回；重跑／舊版升級保持訂閱、網站順序、偏好及核心選擇；Mihomo 下載失敗時保留運行中程序並停止後續切換，原本停止或未初始化的狀態不擅自啟動。[實作入口](../../localclash-luci/openwrt/luci-app-localclash/root/usr/libexec/rpcd/localclash) | LuCI 編排＋Core／按影響 | 2026-10-02 更新至 Core v0.1.93／LuCI 0.1.0-83；FAIL；準備階段 runtime 崩潰，Mihomo 下載失敗仍繼續探測，最後無法恢復；修正候選的主機回歸已通過，iStoreOS 功能回驗 NOT_RUN [E19](#e19)；歷史範圍見 [E18](#e18)，其下載失敗後繼續更新行為不再採用；UI／RPC session 歷史證據見 [E17](#e17) |
| LUCI-TASKS | **介面與長任務交互**：訂閱、網站、初始化及更新頁面操作與後端一致；日誌、取消、互斥、重新連接與終態可用，無重複交易／無限 busy；依各操作是否支援取消驗證。[實作入口](../../localclash-luci/openwrt/luci-app-localclash/root/usr/libexec/rpcd/localclash) | LuCI／共用 | Core `591c357`／LuCI v0.1.0-81 候選；PASS（影響範圍）；真 RPC session 從啟動追蹤至終態，active-task 重裝未建立重複交易，reload 後同 token、ubus method／ACL、HTTP／LuCI 均可用 [E17](#e17)；其他長任務交互沿用 [E05](#e05) |
| LUCI-TAKEOVER | **OpenWrt 網路接管**：從正式入口套用及停止防火牆、策略路由與 DNS 接管；獨立 LAN client 的實際 TCP／UDP／DNS 請求按配置到達受控 WAN endpoint，停止後恢復原資料面，且非本產品規則保留。內部 effective、規則或 lease 只能解釋結果。[實作入口](../../localclash-luci/openwrt/luci-app-localclash/root/usr/libexec/rpcd/localclash) | LuCI／共用 | Core v0.1.96 `d5506d9`／LuCI v0.1.0-85 `d24de8e`；PARTIAL；正式 ARM64 路由器 Smart 的 IPv4 UDP／TCP DNS 接管、Mac 混合大小寫／PTR 與公開 A／AAAA 通過，TCP socket 歸屬證明 53→7874；IPv6 GUA／link-local 通過；正式 LAN 開啟 SLAAC 後原生 Mac ULA UDP／TCP 14/14 與本地 PTR 通過，53→7874 接管及 LAN 回程已確認 [E26](#e26)，取代 [E22](#e22) 的自動來源 ULA FAIL；診斷與 VM 原生 RA 對照見 [E23](#e23)／[E24](#e24)／[E25](#e25)；歷史狀態轉移保留 [E21](#e21) |
| LUCI-RESTORE | **接管及服務恢復**：開機、WAN 事件、受管程序退出及依賴故障後，按使用者意圖恢復或撤回接管；每次轉移後由獨立 client 重跑相同資料面 oracle，明確停止後不自行重開。內部狀態與 lease 不代表服務可用。[實作入口](../../localclash-luci/openwrt/luci-app-localclash/root/usr/libexec/rpcd/localclash) | LuCI＋Core 生命週期／共用 | Core `9881182`／LuCI `18c3b7c` + 發布前候選（binary `596c797d…4d17`、IPK `e5218681…ac7e`）；PARTIAL；停止 Mihomo 後不清理 firewall，租約到期轉 dnsmasq，正式重新授租後完整 IPv4 UDP／TCP 查詢集成功；到期階段僅四項查詢、受控故障敏感度及 IPv6 尚未完整覆蓋 [E21](#e21)；開機／WAN 歷史範圍保留 [E05](#e05) [E11](#e11) |

## 舊記錄的保留方式

舊表中的操作／故障子項歸入相應完整能力，其證據只能支持原本測過的部分。

| 舊條目 | 現行功能／處理 |
| --- | --- |
| SUB-EDIT、SUB-INVALID | SUB-MANAGEMENT |
| SUB-PARTIAL-FAILURE、SUB-ALL-FAILURE、SUB-LARGE | SUB-REFRESH |
| SITE-ROUTING、SITE-INVALID | SITE-ROUTING；一般域名操作是正常功能內容，不新增逐 Bug 任務 |
| INIT-POLICY、UPDATE-REPEAT | LUCI-INIT、LUCI-UPDATE；只保留原版本與部分證據 |
| CORE-CONFIG | CORE-PROFILE、CONFIG-RENDER、CONFIG-VALIDATION、CONFIG-APPLY 的相關情境 |
| CORE-STARTUP、RUNTIME-LIFECYCLE、CORE-VALIDATION、CORE-PAIR-UPDATE | RUNTIME-MANAGEMENT、CONFIG-VALIDATION、COMPONENT-MIHOMO |
| NET-DIRECT、NET-PROXY、NET-DNS-LAN、NET-UDP-DIRECT | 保留 E03／E04 的原始觀測；不把局部網路結果改成 LUCI-TAKEOVER 整項 PASS |
| NET-UDP-PROXY、NET-IPV6、NET-CONTINUITY | 需要時作 LUCI-TAKEOVER／UPDATE 的整合取證，不再列核心傳輸能力功能 |
| 其他安裝、更新、任務、恢復及故障條目 | 按本輪變更選擇相應完整能力中的正常／失敗操作；舊 ID 與結果在原報告保留，不直接轉移整項 PASS |
| 舊 CORE-SELECTION、CORE-STATE 及 Mihomo 模型／演算法／內部狀態條目 | 退出產品驗收範圍，歷史報告保留 |

歷史 NET-UDP-DIRECT：2026-09-08、Core v0.1.81／LuCI 0.1.0-76，
Meta [E03](#e03)／Smart [E04](#e04) 的 IPv4 DIRECT echo 子項 PASS；controller 只有計數而非完整 connection object。
這項結果僅證明該次子項；
整理功能表不會把它改成新版本測試或另一功能的完整通過。

## 首次匯入的證據範圍（2026-09-08）

本節記錄當時只讀取少量既有報告及所列原始證據，沒有啟動 VM、重跑功能或改寫原始報告
結論；當時尚未匯入的其他功能標為「待核對」。後續 E05 已以新候選實測更新全部功能列。
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

<a id="e05"></a>
### E05

2026-09-08 執行了一輪當時稱為「31 項完整候選核對」的測試。主要證據為
[R4 Core／MCP 報告](../.runtime/istoreos-acceptance/20260908-full-functional-v0183-v076-r4/execution-report.md)、
[R5 官方身分／LuCI UI 報告](../.runtime/istoreos-acceptance/20260908-full-functional-v0183-v076-r5/execution-report.md)、
[R6 元件／工作區／恢復報告](../.runtime/istoreos-acceptance/20260908-full-functional-v0183-v076-r6/execution-report.md)、
[R7 LuCI 缺口報告](../.runtime/istoreos-acceptance/20260908-full-functional-v0183-v076-r7/execution-report.md)及
[R8 VDE／DNS 報告](../.runtime/istoreos-acceptance/20260908-full-functional-v0183-v076-r8/r8-report.md)。
實測入口包括 Core CLI、MCP、rpcd／ubus、真實 LuCI 頁面及三台 QEMU 的 VDE LAN；
當時報告為 28 項 PASS、1 項 FAIL、2 項部分通過；2026-09-09 的證據審核已撤回
其中 `LUCI-DNS`、`LUCI-TAKEOVER` 及 `LUCI-RESTORE` 的完整 PASS，見 [E09](#e09)。E05 仍保留為
當時執行紀錄，不能再引用為 31 項能力完整通過。

| 產物 | 候選身分 |
| --- | --- |
| localClash Core linux-amd64 | `v0.1.83`；revision `811c44dee4c69fbd06485c1682104a5f91e924b3`；SHA `312a1abff750f6f8cf882cd7818e6a94ed8bf193a94de8aa75aaa84d354b9290` |
| LuCI IPK | `0.1.0-76`；SHA `9c024307e911e487ce0fbf0c3a9a745a28353c8f511fc2a2f9717654a99cac97` |
| Meta | `v1.19.30`；SHA `20ba567571d9ca642bedecbb01f8092cab0f1679100087ef1a4a2efac0ed5494` |
| Smart | `alpha-smart-651ca46`；SHA `707ec1ce441c62d8083d8b5e147a5f56d78e1b4b9717db6015f8edc6d0d1acdc` |
| iStoreOS | `24.10.8-2026073111` x86_64；SHA `2ce609e2625f9ba67723ec29b0b509baa300c6b74f528596490d950909e09a9c` |

候選身分審核曾發現受控 manifest 把舊 Core SHA `997df496…`／revision
`65db7d69…` 誤標為 v0.1.83；所有由該二進位產生的 Core 相關結果均標為
`INVALID_PROVENANCE` 並排除。表中結果只採用官方 SHA 關閉後的證據；UI-only
證據亦須能對上候選 LuCI 與其實際安裝 Core 的來源鏈。

本輪確認的非 PASS 結果如下：

- `COMPONENT-DASHBOARD`：更新下載失敗後，原有 zashboard 目錄被清空；這是已重現的產品 FAIL。
- `WORKSPACE-RESET`：Core 的 normal／full reset、範圍保留及重建均通過；真實 LuCI「完整重置」按鈕沒有建立確認框、task 或結果，因此只記部分通過並保留入口 FAIL。
- `LUCI-INSTALL`：離線重裝、完整性錯誤及受控版本交接切換通過；不相容 arm64 IPK 令 guest `opkg` 以 SIGSEGV／139 結束，雖未安裝且既有 package／status hash 不變，仍沒有取得明確架構拒絕，記為環境 ERROR／部分通過。

DNS 的正向整合使用受控 producer 只驗 LuCI 對合格結果的套用、生成、驗證及移除，
不冒充 dnsqualify 演算法驗證；另以同一正式 helper invocation 的 `eth0`→`eth9`
變更證明 `dnsqualify_wan_changed` 與舊配置 hash 回滾。接管則從真 LAN client
`192.168.101.10` 對受控 endpoint `192.0.2.3` 發送請求，並確認 apply／stop 及
非產品 nft marker 保留。收尾已停止 runtime、takeover、boot restore、MCP、fixture、
VDE 與全部 QEMU；保留的 qcow2 均通過 `qemu-img check`。

## 功能邊界與尚待核對項

現行 LuCI `overview.js` 的 `bootstrap_default`、`one_click_update`、runtime／
takeover 操作，`subscription.js` 的 `subscription_setup_async`，以及維護頁
`index.js` 的元件、服務及 `reset` RPC 是本表入口依據。
E05 已按候選版本核對這些正式入口及真實 UI；目前不新增健康 S2 運行中跨核心切換、
配置單獨 reset、DNS 資格到期自動處理等不存在的 UI 功能。

DNS lease 現在只跟隨受管 Mihomo 的 supervision state、boot identity、PID 及 executable
identity，不再輪詢 DNS；它不能也不應宣稱代理出口切換期間的 DNS 始終健康。E10 已補
獨立 LAN client 的正向 UDP／TCP oracle 及無 probe capture；進程保持退出至 lease 到期後
的 LAN oracle 仍未執行。host mock 亦不得用來補齊功能證據。

E01–E04 首次匯入仍未窮舉所有歷史目錄或舊版配對；E05 已取代目前 31 項功能的
「待核對」狀態，但不把功能表以外的 UDP 代理、IPv6、效能或全傳輸協定矩陣
納入產品能力。沿用舊證據時仍須檢查同功能、核心及 variant 是否有更晚失敗。

<a id="e06"></a>
### E06

2026-09-09 對 E05 的三個非 PASS 項目修復後做受影響範圍回驗；完整矩陣與原始證據見
[修復回驗矩陣](../.runtime/istoreos-acceptance/20260909-current-fixes/matrix.md)。候選以 Core
base commit `4d5218d9d48abd3b34c8fe0a641ef06c40cc8e98` 與 LuCI base commit
`fb3fe8ccf8de521ee87b0962ad6f8eb5bb8341a3` 加本輪工作樹改動建立；Linux Core SHA-256
為 `3832c19ec4de45e5fca3117bd735028e2471191407b74d30c7cfc9a1e39dd602`，LuCI IPK SHA-256
為 `ad87e939c7cc442b6121107d0718726e186591c3f4c9169e89140212b83b0498`。

- `COMPONENT-DASHBOARD`：可拋棄 guest 的正式 component 入口實際作出 5 次下載嘗試；全部失敗後回傳可見的 optional skip／warning，原 dashboard digest 前後一致。這只關閉失敗保留契約，不宣稱本輪成功取得上游 Dashboard。
- `WORKSPACE-RESET`：以 Arc CDP 操作可拋棄 x86_64 iStoreOS 的真實 LuCI 頁面；checkbox 狀態控制按鈕，點擊後出現進度及成功結果，沒有 native confirm。為保留快速消失的結果 modal，只在瀏覽器端延長 timer 供截圖，沒有改寫 UI 點擊或 RPC。
- `LUCI-INSTALL`：iStore 安裝器及 LuCI 更新 helper 均在 `opkg` 前檢查 IPK metadata；`Architecture: all` 正常通過，arm64 fixture 明確拒絕，未呼叫 `opkg` 且既有狀態 digest 不變。

回驗完成後已停止 QEMU 與 fixture，確認轉發 port、PID、monitor 及 serial socket 均消失；
保留的 guest image 經 `qemu-img check` 通過。本輪沒有擴展到完整 31 項重測或正式路由器驗收。

<a id="e07"></a>
### E07

2026-09-09 對 `COMPONENT-DASHBOARD`、`WORKSPACE-RESET`、`LUCI-INSTALL` 三個完整功能列重新測試，
不沿用 E06 結論；完整矩陣與原始證據見
[三功能重新測試矩陣](../.runtime/istoreos-acceptance/20260909-three-feature-retest-r1/matrix.md)。
候選為 Core commit `11ee2634c70e9a105b019d940be3aa119a84131d`、LuCI commit
`2ac337ff22d21f0bd2d75d965f13b7dd744a6c2e`；Linux Core SHA-256 為
`3832c19ec4de45e5fca3117bd735028e2471191407b74d30c7cfc9a1e39dd602`，LuCI 0.1.0-76 IPK
SHA-256 為 `ad87e939c7cc442b6121107d0718726e186591c3f4c9169e89140212b83b0498`。

- `COMPONENT-DASHBOARD`：正式 Core component 入口以有效受控 archive 完成更新，讀回安裝狀態、`external-ui: ui/zashboard`、controller `/configs` 與 `/ui/` 頁面；另重新執行 5 次下載失敗，確認 typed optional skip、`changed=false`、無假變更及原 index hash 保留。
- `WORKSPACE-RESET`：normal／full 的 dry-run 與 execute 範圍一致，範圍外檔案及非受管程序保留，full reset 後可重新套用模板建立 intent／runtime profile。Arc 真頁面重新驗證 checkbox 初始禁用、勾選啟用、真實 click busy 狀態、HTTP 200 `localclash.reset` RPC、`full_reset_completed` 結果 modal 及無 native confirm。測試中先遇到另一個候選 Mihomo 尚在運行而被安全拒絕；停止該 runtime 後重跑通過，此項保留為前置條件證據而非產品失敗。
- `LUCI-INSTALL`：fresh guest 的離線安裝／重裝讀回 package 與 Core SHA；受控 helper 76→77 更新完成，對舊 76 正確回報 `target_older`。離線安裝器的損壞 checksum、錯誤套件名及 arm64 metadata 均在 `opkg` 前拒絕；helper 對受控 arm64 0.1.0-78 亦回傳 typed `luci_package_metadata_invalid`。`opkg` sentinel 證明兩條 arm64 路徑都沒有 install invocation，訂閱／策略 hash 保留。

Dashboard archive 與 LuCI 77 更新均為受控本地 fixture，只驗產品下載／更新編排，不代表本輪驗證公網 Release 可用性。
初始 serial 長命令及一次不受支援的 BusyBox `find -printf` 只作環境診斷，已以 bounded SSH 與相容讀回重跑，不納入 PASS。
測試完成後已停止 guest 服務、QEMU 與 fixture，確認所有轉發 port、PID、monitor／serial socket 消失；
最終受測 overlay `/tmp/lc-three-r1/istoreos-test.qcow2` SHA-256 為
`abf42f2839ecb35341bed5bf7d2026ecd4b975eff291a2139dbab1fcef696021`，
`qemu-img check` 無錯誤。本輪不評估其餘 28 項功能或正式路由器。

<a id="e08"></a>
### E08

2026-09-09 對 Core `030a7a2` 的 router 預設 TLS sniffer 端口變更執行限定回驗。
`go test ./...`、`go vet ./...`、runtime-profile／config-render 定向測試及 diff 檢查均通過；
獨立測試以可拋棄工作區經正式 CLI render，確認 Meta 與 Smart 的生成配置都精確保留
`[443,465,993,8443]`。完整證據見
[router TLS sniffer 報告](../.runtime/istoreos-acceptance/20260909-router-tls-sniffer-v0185/report.md)。

本輪未使用 Linux Mihomo binary 驗證或 iStoreOS QEMU，記為 NOT_RUN 而非 PASS；
`CONFIG-VALIDATION` 保留 E05 的最後完整實測版本。生成結果不證明正式路由器已載入，
也不證明 macOS Mail.app 的 Gmail 流量會呈現可嗅探 SNI 或命中 Google 代理；這部分保留給發佈後真機驗收。

<a id="e09"></a>
### E09

2026-09-09 重新審核 E05 的接管證據及 LuCI CI。E05 的 VDE LAN client 只驗證一般請求
到受控 WAN endpoint；DNS 部分以 `effective=true`、lease、guard 狀態及 nft 規則判定，
沒有由 client 對 router:53 發出真實 UDP／TCP 查詢。後續正式使用配置又確認 v76 會把
合法的 `noresolv=1` 加明確 WAN server 錯誤拒絕，且 preflight 失敗前已清理既有接管。
受控 producer 亦只證明 helper 可消費合成結果，不能證明真實量測到套用及 client DNS 行為。
因此 `LUCI-DNS` 改記 PARTIAL、`LUCI-TAKEOVER` 改記 FAIL，`LUCI-RESTORE` 只保留非 DNS
狀態轉移的部分證據。

本次審核沒有重跑產品驗收。LuCI CI 中以替身取代 dns-probe、nft、UCI、takeover apply／stop
的三個腳本已移除；其他 host-only contract checks 只能作開發檢查。後續要恢復 PASS，必須在
可拋棄 iStoreOS QEMU 透過正式入口操作，並在已打通的非透明代理 WAN 拓撲中，以獨立 LAN
client 的實際資料面結果作 oracle；不得以新增針對性 mock 或內部 readiness 代替。

<a id="e10"></a>
### E10

2026-09-09 先以公開 LuCI `v0.1.0-77` 在可拋棄 iStoreOS `24.10.8-2026073111`
x86_64 QEMU 重現及界定 DNS 問題，再以 base commit `66d6cc8` 加本輪三個腳本改動所建的
版本 bump 前的候選 IPK `e52abad6be632eec15771148f3149d1693783c2df50ec5d5804cdc72677d72df`
做影響範圍回驗。完整報告見
[v77 DNS 調查](../.runtime/istoreos-acceptance/20260909-v077-dns-dead-r1/report.md)及
[生命週期 lease 回驗](../.runtime/istoreos-acceptance/20260909-v077-dns-lifecycle-r2/report.md)。

- 公開 v77 在原始配置中，Mihomo `127.0.0.1:7874` 的 DNS upstream 在 takeover 前已不可用；
  fail-open lease 沒有授予，dnsmasq 明確 WAN baseline 仍可回答。換成受控可達的普通 DNS
  upstream 後，Mihomo、dnsmasq 及真 LAN client `192.168.101.10` 的 UDP／TCP 查詢全部成功，
  不支持 nft lease redirect 本身必然造成 DNS 死亡的假設。
- 新候選把續租條件改為受管 runtime 的 `running` state、當次 boot identity、PID 存在及
  executable identity；狀態明示 `guard_basis=runtime_lifecycle` 與
  `guard_reason=runtime_lease_refreshed`。20 秒、跨至少三個 renewal interval 的 loopback
  `:7874` capture 為 0 packet，證明 guard 不再製造 UDP／TCP DNS probe。
- 真 VDE LAN client 對 router `192.168.101.1:53` 的 UDP 與 TCP 查詢均 `rcode=0`、各有兩個
  answer，pcap 同時記錄 request／response。外部終止 Mihomo 後，guard 曾觀察到
  `mihomo_runtime_inactive`；Core supervision 隨後以新 PID 恢復，lease 再次續上。正式
  `runtime_stop` 會撤回 takeover、清除 guard state，WAN dnsmasq 仍回答，而 7874 明確拒絕。
- 未把 supervision 停用並強迫進程長期退出，因此「inactive 持續至 lease 到期後的 LAN
  資料面」記為 NOT RUN，不提升 LUCI-RESTORE 為完整 PASS。測試後已停止 runtime、takeover、
  router/client QEMU 與 VDE；兩份 qcow2 均通過 `qemu-img check`，未操作正式路由器。
- 同一組三個腳本改動連同 package release bump 以 LuCI v0.1.0-78、commit `51b323b`
  公開發佈；Main CI `34348679841` 與 Release workflow `34348964548` 成功，13 項公開資產、
  六份 checksum 與兩架構 `.run` 靜態／完整性／解包檢查通過。公開 v78 IPK 未另行安裝到
  QEMU 或正式路由器，因此不把資產驗證寫成新增的 runtime 功能證據。

<a id="e11"></a>
### E11

2026-09-09 以 Core `8d573f3`、LuCI v0.1.0-79 source `005f59e` 建立候選 IPK
`a150ece03b7ae7b94b88b1661c60370c9a2c02cc83330ce9cc12074fcb14964d`，在可拋棄
iStoreOS `24.10.8-2026073111` x86_64 QEMU 對 ingress dead-man DNS lease 做影響範圍回驗。
完整報告見 [ingress dead-man DNS 回驗](../.runtime/istoreos-acceptance/20260909-ingress-deadman-210514/report.md)。

- dnsmasq 使用 `noresolv=1` 與明確 catch-all WAN server；隔離 WAN namespace 先獨立證明
  UDP／TCP DNS 都可回答。lease active 時，router localhost 與獨立 LAN client 的硬編碼外部
  DNS UDP／TCP 查詢均取得 Mihomo 回應並命中 `:7874` counter；對 router DNS 地址的 LAN 查詢
  保持 dnsmasq bypass。狀態如實顯示 `mode=ingress_deadman`、`fallback=dnsmasq` 及 `path=mihomo`。
- 不執行 takeover cleanup，只停止 dns-guard 並等待超過 15 秒。active sets 自動清空後，以新
  query name／TCP connection 建立的新 router 與 LAN UDP／TCP flow 均由 dnsmasq 取得 WAN fixture
  答案 `1.2.3.4`；`:7874` counter 不再增加，LAN permanent `:53` redirect counter 增加。此結果
  接受 nft NAT／conntrack 對既有 flow 的限制，不把舊映射當恢復 oracle。
- kill 受管 Mihomo 並等待 lease 到期後，新 UDP flow 亦保持 dnsmasq fallback；status 顯示
  runtime／effective false、DNS path `dnsmasq`。stop→apply→重新授租及最終 stop 通過；localClash
  nft／policy route 清除，UCI 與 generated dnsmasq config hash 前後相同。
- QEMU slirp `10.0.2.3` 沒有可用的 TCP DNS baseline，因此該早期觀測不列入功能結論；最終
  UDP／TCP oracle 使用隔離 WAN namespace。測試後 runtime、guard、fixture、namespace、QEMU
  均已停止，qcow2 通過 `qemu-img check`。未操作正式路由器，亦未宣稱 ARM64 runtime 驗收。

<a id="e12"></a>
### E12

2026-09-16 對 Core `4bdd1d4` 的 Smart/router 候選執行限定驗收；LuCI 未修改，QEMU 映像內既有
LuCI 為 `0.1.0-76`，因此不作 LuCI v0.1.0-79 新版宣稱。原始證據見
[v0.1.86 候選報告](../.runtime/istoreos-acceptance/20260916-release-v0186/report.md)。

- 正式 `subscription refresh` 取得並解析受控 fixture 的 50 個真實 VLESS 節點，生成 v7
  `openai.chatgpt.oauth_statsig.v1` snapshot；11 個節點符合 OAuth region-supported 與
  Statsig reachable 的交集，39 個不可用。先停止必要 fixture endpoint，正式 refresh 以
  `all subscription sources are invalid` 失敗；恢復後再次成功，確認 oracle 敏感度。
- 正式 `config apply-template`（Smart/router/default）、`config render`、候選配置複製及
  `mihomo config-test`／`config-promote` 均成功。候選 SHA 為
  `2cf720ee61445d0a9c8e99adf6b1a6aed754e7b79ee3038e2aa83acc9cde085e`；正式 controller
  read-back 的 `ChatGPT-available` 11 名與 v7 snapshot 完全一致。
- 獨立逐出口 HTTP oracle 對 US04 觀察到 OAuth `401 token_expired` 與 Statsig `200 br`；
  snapshot 當時記錄的 TW02／SG02 為 OAuth `403 unsupported_country_region_territory` 且
  Statsig `200`，但較後獨立重測未重現該 OAuth reject（回為 `401 token_expired` 或 transport
  error）。故 SUB-REFRESH 記 `PARTIAL`，不得把這次時間漂移升格為完整 PASS；其餘四個選測
  配置功能依本輪正式入口與外部可觀察結果記 `PASS`。

<a id="e13"></a>
### E13

2026-09-16 對 v0.1.87 候選（base `9a36f6b` 加本輪改動）執行 template-refresh
限定回驗；原始證據見 [回歸驗收報告](../.runtime/istoreos-acceptance/20260916-template-refresh-regression/report.md)。
候選 Linux amd64 binary SHA-256 為
`2596cf4d4de38188931f0b3a3aeac7eb2f5a263339ef3e55a21cc588fd2bfa57`；環境為
iStoreOS `24.10.8-2026073111` x86_64、LuCI `0.1.0-76` 及 Mihomo Smart
`alpha-smart-651ca46`。

更正：此輪舊狀態由候選環境直接修改檔案產生，並非已發布舊版 Core 經正式入口留下的
workspace；因此只保留為 synthetic regression，不構成 v0.1.86→v0.1.87 升級證據，亦不再
作為上述功能列的最新實測依據。

- 正式 `subscription set --json` → `subscription refresh --json` 從保留
  `policy_template=localclash-default`、但含舊 `ChatGPT-old`／
  `openai.chatgpt.statsig.v1` 的狀態開始。刷新後舊 template-owned group 不存在，當前模板的
  `ChatGPT-available`／`openai.chatgpt.oauth_statsig.v1` 出現；過程沒有舊名查找、遷移或 alias。
- user-owned `user.keep` patch 及其 `User-owned-preserved` group 保留。正式 capability 探測完成
  50 個候選，觀察到 OAuth unsupported 2、Statsig reachable 14，交集 qualified 12。
- 本輪只關閉 template-owned group 重建回歸；沒有新增獨立逐出口負向 oracle，因此
  `SUB-REFRESH` 保持 `PARTIAL`。兩次前置失敗分別是非 canonical 測試 patch filename 與 fixture
  server 提前退出，修正測試材料後正式入口通過，不列產品失敗。QEMU／fixture 已停止，overlay
  通過 `qemu-img check`。

<a id="e14"></a>
### E14

2026-09-16 以同一個可拋棄 iStoreOS x86_64 qcow2 重做 persisted upgrade；原始證據見
[真實升級驗收報告](../.runtime/istoreos-acceptance/20260916-v0185-to-v0186-upgrade/report.md)。

- 已發布 v0.1.85 經正式 reset／template／subscription／refresh／render／config-test／promote／
  runtime／controller 路徑建立並使用舊 workspace；只升級到 v0.1.86 後，正式 refresh 以
  `unsupported proxy-group capability: openai.chatgpt.statsig.v1` 失敗，重現原升級缺陷。
- 權威鏈路沒有在 v0.1.86 失敗後 reset 或人工修復：同一個 v0.1.85 workspace 保留舊 capability
  與 user-owned custom site，直接替換已校驗的 v0.1.87 binary／版本資產。v0.1.87 正式 refresh
  成功，把 template-owned `statsig.v1` 重建為 `oauth_statsig.v1`，原 user state hash 不變；正式
  save/apply、render、Mihomo `-t`、runtime 及 controller read-back 均通過。
- 連續鏈路記錄三版 binary、base-assets SHA 與各 transition 前後 state hash。v0.1.85→失敗的
  v0.1.86→v0.1.87
  對 `CONFIG-TEMPLATE/RENDER/VALIDATION/APPLY` 為 `PASS`；`SUB-REFRESH` 因未取得同輪獨立 OAuth
  POST 正／負 oracle 維持 `PARTIAL`。QEMU 已停止，qcow2 通過 `qemu-img check`。

<a id="e15"></a>
### E15

2026-09-18 對 Core v0.1.88 候選 `16af45b` 的 Xet suffix 路由執行限定升級驗收；原始證據見
[v0.1.88 Xet suffix 驗收報告](../.runtime/istoreos-acceptance/20260918-v0188-xethub/report.md)。
候選 Linux amd64 binary SHA-256 為 `42e8356d67b0b86f5cf8031d6ac7ba916c0a7fb0bc3f43259f8b5b196a98ba42`，
base assets SHA-256 為 `153571fb96db5504e4cb11a4f89ff42ab4b91889000a5bb1c5dad2e568e74166`；
環境為 iStoreOS `24.10.8-2026073111` x86_64 與 Mihomo Smart `alpha-smart-651ca46`。

- 同一個新 qcow2 先安裝公開 v0.1.87 binary／匹配 base assets，經正式 `reset --full` 清除原
  workspace，再由 v0.1.87 正式套用預設模板、取得受控 subscription fixture、refresh、render、
  Mihomo `-t`、save/promote、runtime 及 controller 讀回，建立 4 條 Xet 精確主機的舊版基線。
- 停止舊 runtime 後只替換候選 v0.1.88 binary 與 base assets；正式模板刷新、訂閱刷新、render、
  config-test、save/apply、runtime 及 controller 讀回全部通過。載入規則為
  `DomainSuffix xethub.hf.co`／`xethub-eu.hf.co` 先於 `GeoSite category-ai-!cn`，因此
  `cas-bridge.xethub.hf.co` 命中「📥 大模型下载」；HF CDN、ModelScope、Ollama 精確規則保留。
- v0.1.87 建立的 user-owned custom-site 規則在升級後仍存在。測試未操作正式路由器；Meta、
  ARM64 runtime 與實際公網下載吞吐未在本輪驗收。runtime、fixture 與 QEMU 已停止，兩份 qcow2
  最終均由 `qemu-img check` 確認無錯誤。

<a id="e16"></a>
### E16

2026-09-18 以 Core `591c357` 與 LuCI base `005f59e` 加本輪工作樹改動，移除 Core
`dnsqualify.json` 讀取 seam、LuCI DNS 最佳化 UI／RPC，以及一鍵更新的 dnsqualify gate。
Core `go test ./...`、`go vet ./...` 通過；configrender 回歸證明不存在、有效及損壞的殘留
`dnsqualify.json` 產生 byte-for-byte 相同配置。LuCI 全部 JavaScript syntax／UI tests、
dns-guard contract 與現存 rpcd／hotplug host checks 通過，一鍵更新 trace 與結果不再包含
dnsqualify。

其後在可拋棄 iStoreOS `24.10.8-2026073111` x86_64 QEMU 安裝候選 Core（SHA-256
`a985b867…0193`）及 LuCI `0.1.0-79`。在 workspace 依序放置不存在、有效及刻意截斷的
`dnsqualify.json`，三次正式 `config render --json` 皆成功，生成配置 byte-for-byte 相同且
SHA-256 均為 `64ce99f8…14f0`；三次 isolated Smart Mihomo config-test 亦通過。損壞 JSON
原檔 SHA `b18193d8…7609` 全程未變，證明 Core 將它視為不存在，而非解析、遷移或清除。

以損壞 JSON 與舊 `/usr/local/bin/dnsqualify` 同時殘留的狀態，透過已安裝 helper 正式執行受控
一鍵更新；任務 `exit_code=0`，Core、Mihomo、dashboard、subscription refresh、render 與
config validation 完成。helper log、task status 與 task result 均無 `dnsqualify`，兩份殘留
artifact hash 不變；runtime 原本停止且完成後仍停止，takeover 保持未生效。已安裝 RPC method、
ACL 及 LuCI JavaScript 亦無 dnsqualify surface。原始證據見
[dnsqualify 退役驗收報告](../.runtime/istoreos-acceptance/20260918-dnsqualify-retirement/report.md)。
本輪沒有正式路由器與瀏覽器視覺操作，故 UI 僅以安裝內容及 RPC surface 驗證，不宣稱視覺驗收。

<a id="e17"></a>
### E17

2026-09-18 以 LuCI base `7d53855` 加本輪工作樹候選 IPK（SHA-256
`32b26cc770a9618ca8f098ece5692183275f084ec99b310fdadd810acaf0cdac`），在可拋棄 iStoreOS
`24.10.8-2026073111` x86_64 QEMU 驗證 rpcd reload 與 Web／session 連續性；原始證據見
[RPC reload 驗收報告](../.runtime/istoreos-acceptance/20260918-rpc-reload-213305/report.md)。

- 無活躍任務時，正式 `opkg install --force-reinstall` 由 post-install 同步執行
  `rpcd reload`；有真 LuCI RPC 一鍵更新任務時，post-install 只寫
  `rpcd-reload-required`，沒有 restart rpcd 或 uhttpd。兩條路徑均安裝成功。
- 真 RPC 一鍵更新先持久化 `running=false`、`done=true`、`exit_code=0`，其後 helper 才 reload
  rpcd 並清除標記；同一 LuCI session token 在 reload 後仍能成功呼叫 `localclash.status`。
- rpcd PID 全程為 `3854`，uhttpd PID 全程為 `6937`；HTTP root、LuCI 與 `/ubus` reload 後可用，
  ubus method 與 ACL 讀回符合候選。未觀察到 nginx／uwsgi 或任何 Web server restart。
- 精確 sub-second reload 窗口未以連續外部 HTTP 探針取樣，列為剩餘風險；不宣稱零瞬斷。
  v0.1.0-81 release candidate 只變更 package release metadata，解包後全部安裝檔案與 E17 QEMU
  候選 byte-for-byte 相同；公開 v0.1.0-81 IPK 再次確認相同。Main CI `35353208725`、Release
  workflow `35353343689`、8 項公開資產、sidecar checksums 與兩架構 iStore bundle 靜態驗證均
  通過。QEMU 已停止，qcow2 經 `qemu-img check` 確認無錯誤。

<a id="e18"></a>
### E18

2026-09-20 以 Core `f23554d` 與 LuCI `a25a3cc` 的乾淨候選，在可拋棄 iStoreOS
`24.10.8-2026073111` x86_64 QEMU 執行 Mihomo 更新與一鍵更新時序驗收；完整報告與原始證據見
[一鍵更新批量提交驗收報告](../.runtime/istoreos-acceptance/20260920-oneclick-batch-runtime-r1/report.md)。
Core binary SHA-256 為 `a47f2fa1…5e69`，LuCI IPK SHA-256 為 `01cfc559…b37c`；活躍核心為
Meta `v1.19.31`，並核對 Smart `alpha-smart-651ca46` 的成對 SHA。

- 每次正向操作前，真實訂閱均由正式路由器經 `subscription get` 只讀串流到 VM 的正式
  `subscription set`／`refresh`；有效重跑確認三個 source 為 30／20／76 proxies、合併 126，
  `assert-update-ready` 證明 runtime running、takeover effective、DNS path Mihomo。獨立 LAN
  client 對受控 endpoint 的 TCP／UDP／DNS oracle 先經故障敏感度檢查，再用於所有有效轉移。
- changed path 先完成 Mihomo、真實訂閱、render 與 final config-test，最後只做一次
  `process_restart`；PID `6424→16254`，連續 300 組資料面全 PASS。identical pair 回報
  `changed=false`，只做一次 final hot reload、PID 不變，連續 220 組全 PASS。
- Mihomo GitHub 取得受控失敗時保留現有核心，整體以一項 warning 成功，不做 process restart；
  缺少兩個 managed core 時相同故障明確失敗。候選 manifest 準備失敗亦未提前 restart，PID、
  takeover、DNS path、核心／訂閱 SHA 與 LAN 資料面保持。
- 首次 changed attempt 遇到真實 source EOF，另一次負向 fixture 漏 LuCI checksum sidecar；兩者
  均保留為環境 ERROR，修正環境後才取得上述結果。未操作 LuCI UI 或 ubus-RPC dispatch，亦未
  執行 final config-test 故障子項，因此 `LUCI-UPDATE` 本輪記 PARTIAL，不把 helper 任務冒充完整
  UI 驗收。測後正式停止 runtime/takeover，router/client QEMU、VDE、endpoint 與 fixtures 全部
  停止，兩個 qcow2 均通過 `qemu-img check`；正式路由器未修改。

<a id="e19"></a>
### E19

2026-10-02 正式路由器的一鍵更新（Core v0.1.93／LuCI 0.1.0-83）出現準備階段
runtime 崩潰；本次撤回 COMPONENT-ASSETS 與 LUCI-UPDATE 的現行通過結論，保留歷史
證據的實際版本與覆蓋。使用者已確認儲存硬體會造成磁碟二進位 bytes 漂移，不能把舊檔存在
當作可重新啟動的證據。

- 更新日誌顯示 18:39:31–36 安裝基礎資源；路由器 mihomo.log 的最後運行日誌為
  18:39:34，其後出現 SIGBUS，堆疊為 MaxMind MMDB → ASNReader.LookupASN。
  該版本的 assets 解壓以 O_TRUNC 原地寫入 ASN.mmdb，與正在使用的 mmap 崩潰機制吻合。
- 隨後 Mihomo、Dashboard 與訂閱請求出現 `[::1]:53 connection refused`。
  18:39:40 watchdog 因 core_hash_mismatch 阻止恢復；18:39:48 的隔離 Smart 探測在初始化
  要求約 24 GiB 記憶體而退出，kernel 亦記錄分配被拒絕。磁碟損壞的具體原因不以本輪
  軟體回歸測試作硬體判定。
- LuCI 原先把 Mihomo 下載失敗轉為 existing_core_preserved 後繼續更新，最後只輪詢
  runtime 六次而無法恢復。修正候選直接保留原始失敗並停止後續探測、配置驗證、runtime
  切換與接管恢復；下載成功仍按既有材料驗證及單一最終切換流程執行。
- Core 修正候選對資產逐檔使用同目錄暫存檔、完整寫入與 sync 後 rename，保留舊 reader／mmap
  的 inode；不宣稱整個資產包是單次原子交易。既有開檔、跨頁 mmap 與不完整 tar 回歸通過；
  不完整 tar 測試在修正前版本能重現原檔被破壞。`go test ./...` 通過。
- LuCI 一鍵更新及 helper 交接主機回歸、shell 語法、BusyBox ash 失敗分支與 IPK／APK 打包
  通過。上述均為主機層證據，不能升格為 iStoreOS／正式路由器資料面驗收。

本輪可拋棄 iStoreOS 的選測範圍為運行中基礎資源更新及 Mihomo 下載失敗時的程序／資料面
持續性；候選身分與原始證據保存在 `.runtime/istoreos-acceptance/20261002-update-continuity/`。
主機驗證與補丁 SHA 見[驗證紀錄](../.runtime/istoreos-acceptance/20261002-update-continuity/host-verification.md)。
本輪 guest LuCI／SSH 可用，正式 subscription-sync 回報 configured=true、merged=true、sources=3、
proxies=30；assert-update-ready 因 running runtime、effective takeover 與 Mihomo DNS path 未建立而
失敗，未執行一鍵更新或獨立 LAN oracle，兩個選測功能的候選回驗均記 NOT_RUN。
[VM 選測紀錄](../.runtime/istoreos-acceptance/20261002-update-continuity/vm-verification.md)保存具體前置與
證據；不提高最後實測版本。測試 VM 已停止、qcow2 check 通過；正式路由器未部署修正。

本修正已發佈為 [Core v0.1.94](https://github.com/qoli/localClash/releases/tag/v0.1.94)
（tag commit `6d6271a`）及 [LuCI v0.1.0-84](https://github.com/qoli/localclash-luci/releases/tag/v0.1.0-84)
（tag commit `18c3b7c`）。Core Release `37000528474`、LuCI Main CI `37000884350` 與
Release `37001068715` 均成功；公開資產／checksum／兩架構離線包已核對，詳細發布驗證見
[更新日誌](changelog.md#2026-10-02)。LuCI 離線包的 Core manifest 固定 SHA-256 為
`d3ff22af7d99b796436d5c2ca53d9a15de0239bfb1976b784a54188f8fe13c3c`。
此處只補發布身分及資產證據，選測功能回驗仍為 **NOT_RUN**，不提高最後實測版本。

<a id="e20"></a>
### E20

2026-10-03 在可拋棄 iStoreOS 24.10.8 x86_64 QEMU，選測 Core v0.1.95
（tag commit `fa94540`）的 CONFIG-RENDER 與 CONFIG-VALIDATION；完整報告與原始證據見
[private DNS 配置升級驗收](../.runtime/istoreos-acceptance/20261003-private-dns-v0195/report.md)。

- 先以公開 v0.1.94（binary SHA-256 `d05b3140…fa95`）正式建立並使用 workspace；
  由正式路由器只讀匯出全部真實訂閱，guest 正式 set／refresh 回報 3 sources、30 proxies。
  baseline router/Meta 配置正式生成及 isolated config-test 通過，SHA `73e1f62b…f241`。
- 保留 workspace，只替換公開 v0.1.95 binary（SHA `c06bc08c…95a7`）及官方 base assets。
  正式刷新與生成成功；獨立讀回確認 `geosite:private -> 192.168.6.1#DIRECT`、
  `direct-nameserver-follow-policy: true`，且沒有物化 DIRECT proxy group。
- Mihomo Meta v1.19.32（SHA `6de24119…3819`）正式 isolated config-test 對候選配置
  SHA `9a4dcbee…e350` 回報 passed=true。兩項功能僅就上述配置生成／驗證範圍記 PASS；
  未啟動接管、租約轉移或獨立 LAN DNS oracle，不宣稱實際 DNS 行為驗收。
- 初次舊版完整模板交易出現 `material transaction path "." is invalid or duplicated`，
  改由正式非交易模板入口建立；缺少核心／能力快照的前置錯誤在正式 core download 及
  refresh 後解除，原始錯誤保留，沒有算入候選通過證據。
- QEMU 已停止，本輪 qcow2 經 check 確認無錯誤後清理；正式路由器未修改。

發布 CI、公開資產與正式路由器 Smart 隔離 `-t` 的補充證據見
[更新日誌](changelog.md#2026-10-03)。CI 與隔離檢查不取代此處的 iStoreOS 操作範圍。

<a id="e21"></a>
### E21

2026-10-03 修復路由器自身 DNS 地址的 LAN ingress 漏接管，以及混合大小寫單節 DHCP
名稱的預設策略。候選為 Core base `98811829e5cf4418ffdcce4ba079e182ce8556c0` 與
LuCI base `18c3b7c31f1d5e09fbdbb2ea3b9f77535db8d7ea` 加當時未提交修正；下列為發布前候選實測，發布對應見本節末。
[本輪完整紀錄](../.runtime/istoreos-acceptance/20261003-router-dns-ingress-fix/report.md)
與 [主機驗證](../.runtime/istoreos-acceptance/20261003-router-dns-ingress-fix/host-verification.md)
保留候選身分、操作及原始輸出。

- 候選 Core binary SHA-256 `596c797d5186cd7ae7e4d87ca4c8b3264a0495ecaab320c518bad8ffaaaf4d17`；
  IPK `e5218681a292cd4fdcb8189d26409d2eb76bb5cab531065aa6468c77d5d8ac7e`；
  APK `a34ab07093734f892f981cffbbe011927adcac307fc02f54a0489bbb76690d49`。
- 可拋棄 iStoreOS 24.10.8 QEMU 先用公開 Core v0.1.95／LuCI 0.1.0-84 正式建立並使用
  workspace；從正式路由器只讀同步全部 3 個真實訂閱來源，刷新得到 107 proxies。
  保留同一 workspace 後，只替換候選 Core／LuCI 資產，再由正式入口生成及驗證。
- Meta v1.19.32 正式 config-test 通過；候選配置 SHA
  `58595e0f11ded35ab40411c5e2beb4bdd6aefd5bd5f3e716de3ed77a1a4889ef`。
  單節 `*`／`geosite:private` 指向 `192.168.6.1#DIRECT`，follow-policy 保留。
- 獨立 VDE LAN client 的 DHCP 名稱為 `Mac`、IPv4 為 `192.168.6.174`。
  租約有效時，直接查 `192.168.6.1:53` 的 UDP／TCP `Mac`、`MAC`、`Mac.lan`、PTR
  及 `mcp.notion.com` 均成功；指定 UDP／TCP 探測前後 active redirect counter 由 10 增至 12。
  本機回查 dnsmasq 的成功由上述 DHCP／PTR 外部回應佐證，dnsmasq 上游沒有改接 Mihomo。
- 正式停止 runtime 而保留接管 firewall，15 秒租約到期後狀態轉 dnsmasq。
  到期階段實際只驗證 MAC/UDP、Mac.lan/TCP、PTR/UDP、公開域名/TCP 四項，不能宣稱
  完整等價類回驗；正式 runtime_start_takeover 重新授租後完整 IPv4 查詢集再度成功。
- 首次準備出現 guest Core／opkg status 零位元組；原始錯誤保留，重裝公開 Core、
  由 guest /rom 恢復 package database，再正式安裝 LuCI 與刷新後解除。其成因只記環境推測，
  不當作候選缺陷或產品 PASS。受控 WAN `10.0.2.2:15353` refused 與 IPv6 未執行亦保留；
  在補齊之前，主代理將 LUCI-TAKEOVER／RESTORE 記為 PARTIAL。
- `go test ./...`、LuCI shell 語法、deadman／restore 主機回歸、IPK／APK 建置及驗證通過。
  Mac checkout doctor 因未初始化 runtime／訂閱／配置而 fail，不列功能通過證據。
  首次 QEMU overlays 經 qemu-img check 無錯誤後清理；正式路由器未修改。
- [全新補測 attempt](../.runtime/istoreos-acceptance/20261003-router-dns-ingress-fix-supplement/report.md)
  另建 router／LAN client／受控 WAN endpoint。router 直查 endpoint 的 UDP A／AAAA
  可取得指定記錄，但 TCP 探測零位元組、dnsmasq／client 轉發 timeout，SSH 管理亦
  timeout／reset。補測未進入公開 baseline 安裝、真實訂閱同步或候選 runtime，屬環境
  ERROR，不能增加候選功能覆蓋，也不能算候選產品 FAIL；IPv6 與故障敏感度仍 NOT_RUN。
  三個 overlays 均經 qemu-img check 無錯誤後清理；完整接管／恢復驗收維持 PARTIAL。

本輪候選後續發布為 Core [v0.1.96](https://github.com/qoli/localClash/releases/tag/v0.1.96)
（tag commit `d5506d9`）與 LuCI [v0.1.0-85](https://github.com/qoli/localclash-luci/releases/tag/v0.1.0-85)
（tag commit `d24de8e`）。Core 公開 binary revision 與 tag 相符，發布 GeoSite 與受測材料
SHA 相同；LuCI 公開 IPK 的 `takeover-apply` 與 QEMU 候選逐位元一致，helper SHA-256
`6c017ee8904349a6f7d30e8bd846e681b2514a3b77fa92ea060298a732564207`。
LuCI 版本號與離線包 Core pin 的變更只作發布身分及資產校驗，不提升功能結果。
發布驗證見 [更新日誌](changelog.md#2026-10-03)；正式路由器未在本輪部署。

<a id="e22"></a>
### E22

2026-10-03 使用者更新正式路由器後，依明示要求驗收正式 ARM64 現場的 DNS 鏈路。
這輪為正常運行中的只讀檢查，使用獨立 Mac LAN client；沒有改用 QEMU、重建產品現場、
停止 runtime、改 UCI／配置／firewall／route、清 cache、讓租約到期或做故障注入。
[完整正式路由器驗收](../.runtime/dns-private-acceptance/20261003-live-v0196/report.md)
保留 client raw、nft、socket ownership、選定 DNS 封包與失敗解釋；client 測試由 Luna High 執行。

- 安裝 Core v0.1.96 arm64 的 SHA `2ca54d19…bfb28` 與公開資產相同，LuCI 為 0.1.0-85，
  helper SHA `6c017ee8…64207` 與發布／QEMU 候選相同。Core MCP 確認 Smart
  `alpha-smart-dc90210` PID 3831，builtin router profile 無 user override。
  配置 SHA `40037288…e2fb`，包含單節 `*`／private 及 direct follow-policy；
  正式 nft 為 lo-only bypass，IPv4／IPv6 lease active，7874 雙棧 UDP／TCP listener 存在。
- Mac `192.168.6.119`／en1 的系統 DNS 為 `192.168.6.1`。直接查 router :53 的 UDP／TCP
  `mcp.notion.com` A／AAAA 均 NOERROR、各四筆地址，UDP 86.7–91.6 ms、TCP 7.7–7.8 ms。
  `Mac`／`mac`／`MAC`／`Mac.lan` 均回 `.119`，PTR 回 `Mac.lan`，與租約一致。
- 獨立持續中的 TCP probe 地址仍為 `192.168.6.1:53`；router /proc/net/tcp6 上同一
  remote IP／client port 的 accepted socket 是 :7874，其 inode 歸屬 Mihomo PID 3831。
  選定封包亦確認本地域名/PTR 經 router loopback :53 回查 dnsmasq 後返回 client，沒有循環。
  此 capture 沒有觀察到選定 Notion／nonce 查詢送向兩個電訊商 resolver 的 port 53；
  未解密或逐筆辨認 DoH 交易，不把配置意圖代替上游封包證據。
- 系統 getaddrinfo 五次成功，約 2.8–22.3 ms；HTTPS root HEAD 回 HTTP/2 404，證明
  DNS／connect／TLS／HTTP 可達，不聲稱 Notion 應用 API 已驗收。
  原本 IPv4 router DNS 漏接管／本輪系統解析逾時問題就上述範圍記 observed PASS。
- IPv6 router GUA 與 link-local 上的 UDP／TCP Mac A 與 Notion A 共八次成功，4.4–11.1 ms。
  ULA 上四次實際查詢逾時，必須記 observed FAIL，不能記 NOT_RUN。Mac 使用 fd82 前綴，
  router LAN route 為 fd0a；選定 UDP DNS 回包已產生但從 PPPoE WAN 送出，route lookup
  回 Mac ULA 則 Network unreachable。這是已觀察的回程失敗，尚未確定該前綴來源或
  選 WAN 的具體 policy；未擅自改 route。初次 client report 的 NOT_RUN 分類已撤回。
- `neakasa-M1`／`.lan` 回 `.244`，但 lease file 列為 `.128`；router 本機直接查 dnsmasq
  亦回 `.244`。此名稱的權威映射不一致仍保留，不能宣稱該 lease-IP 斷言通過。
- LUCI-TAKEOVER 整項保留 PARTIAL，列出上述 ULA FAIL 與未驗狀態轉移；LUCI-RESTORE
  未在正式現場重測，保留 E21 的實際版本及限制。使用者的更新交易本身未被觀察，
  本輪不改 LUCI-UPDATE 的歷史分類。ARM64 現場 DNS 運行證據不代表全功能 ARM 驗收。
  臨時 AF_PACKET observer 到時退出，/tmp binary 已移除，正式配置 SHA 前後相同。

### E23

2026-10-03 至 10-04，依使用者要求在既有 iStoreOS QEMU 測試環境建立獨立 ULA
對照；重用 managed router overlay，新增獨立 VDE LAN client，未修改正式路由器。
[對照報告及原始證據](../.runtime/istoreos-acceptance/20261003-ula-comparison/report-attempt2.md)。
此為回程路由診斷，補充 E22，不撤回正式現場已觀察到的 ULA FAIL，亦不升格
LUCI-TAKEOVER 整項 PASS。

- VM 安裝公開 Core v0.1.96 amd64（SHA `34ba6c9e…f3f3f86a94`）與 LuCI
  v0.1.0-85（IPK SHA `65362536…839aad30`）。Smart 為乾淨 `dc9021073025`
  原始碼的 Linux amd64 診斷 build，Go 1.26.0、`with_gvisor`、`vcs.modified=false`，
  SHA `082fa511…38b0ded`；與正式現場 source revision 相同，但不是公開 Smart
  資產或 ARM64 binary 的等同性驗收。
- 測試 ULA 為靜態別名：router `fd0a:67b1:7933::1`；client 同前綴
  `fd0a:67b1:7933::119`／不同前綴 `fd82:eade:488e:44bd::119`。實際獨立
  client 固定來源，經 IPv6 kernel／VDE 傳送 UDP／TCP，未聲稱 DHCPv6／RA
  自動分配這些測試地址。
- dnsmasq 關閉接管的同前綴矩陣 12/12、Smart lease 生效後同前綴矩陣 12/12
  均為非空 NOERROR；包括 Mac 混合大小寫、本地受控 host-record 與 Notion A。
  不同前綴有明確 LAN 回程時，第一輪 dnsmasq 與第二輪 Smart 各 6/6 成功；
  移除回程時各 6/6 逾時。Smart 恢復路由後 6/6 成功，最後 dnsmasq 與 Smart
  的 Mac UDP／TCP 恢復探測亦均成功。不同前綴的 dnsmasq 對照在 VM 暫時
  設 `localservice=0`，區分其本地來源 ACL 與回程失敗。
- 正式 helper 的 active lease／`dns.path=mihomo`、7874 listener 與 DNS redirect
  counter 已記錄。最初尚未證明 lease 的成功矩陣不單獨作 Mihomo 證據；
  client 地址仍 tentative／未可 bind 的錯誤與 LAN bridge 未接妥的逾時，均保留
  為測試前置錯誤，修正後重測。
- 刻意把 client 回程設為 WAN 後，Smart 同一 UDP DNS ID `65530`／client port
  `44598` 在 br-lan 僅見 request，在 eth0 見正確 `Mac A 192.168.6.119` reply；
  同時 utun capture 無匹配封包且三處均無 kernel drop。另見 WAN TCP SYN-ACK，
  不能算 TCP DNS answer。支持本機 DNS 回覆直接選錯出口；這個 QEMU 模型不能
  單獨確認正式路由器究竟哪項 policy／路由造成選 WAN。dnsmasq 的錯誤 WAN
  路由探測只有 client 逾時證據，沒有相同 UDP WAN 回包的 capture 證明。
- 測試工作區原有 12 個空模板 patch 已備份及隔離，再透過公開 base-assets／
  正式 apply-template 重建。正式 subscription-sync 匯入全部 3 個 configured
  sources；後續刷新明示 source 02 HTTP 403 且無有效 cache、被跳過。
  因此完整 all-source 正向產品現場前提仍為 NOT_RUN，不能以 configured count
  或 CLI success 宣稱全部 sources 刷新成功。本地 host-record 也不是受控 WAN DNS
  endpoint，完整 WAN oracle／release upgrade acceptance 未在本輪完成。
- guest sync 後關閉測試 VM；兩個 overlay 的 qemu-img check 均無錯誤，僅移除
  本輪新增 client overlay，保留原 managed router 及全部原始失敗／恢復證據。

### E24

2026-10-04，依使用者同意繼續只讀定位正式 LAN 的 ULA 來源及實際 DNS 回程。
[完整來源／NAT／回程報告](../.runtime/dns-private-acceptance/20261004-ula-origin/report.md)。
本輪是診斷取證，未修復或撤回 E22 的正式 ULA FAIL；不改 LUCI-TAKEOVER 的
PARTIAL、RESTORE／UPDATE 的歷史分類。

- Mac `ndp` 直接列出 fd82 前綴的三個 RA 宣告者；mDNS Thread border-router
  service、host AAAA 與 link-local／MAC 一一對應到 Apple `客廳`、`客廳電視`、
  `睡房`。主路由器宣告 fd0a 與 GUA，但實際 RA 的 prefix option 沒有 A flag，
  與 `ra_slaac=0` 相同。Mac fd82 為 en1 SLAAC，不是 TUN 位址。
- 最新 Mac DHCPv6 REPLY 同時提供 GUA 與 fd0a 地址，stateful address 卻只有
  前面的 GUA，與 Apple 公開 client 選第一個有效 IAADDR 的實作一致。公開源碼
  未作本機 OS binary 的精確版本 attestation。不能把缺少 fd0a 簡化為 DHCP server
  沒有提供 ULA，或假設再等 lease 刷新即可修復；原始 reply／RA／地址狀態均保留。
- Router `br-lan accept_ra=0, forwarding=1`，當前全路由表沒有 fd82 LAN 回程。
  先前以線上來源 fd0a 的 route lookup 是 unreachable，但 NAT 前 GUA source
  的 lookup 走 source-specific PPPoE WAN default。
- 55 秒臨時只讀 observer 以 pidfd duplicate 取得 PID 3831 fd 8 metadata：
  UDP `:::7874`、SO_MARK 0、未 connect；沒有讀寫原 socket 佇列或暫停 runtime。
  新的 Mac／Notion UDP 查詢與正確回覆逐筆對上 DNS ID／port，回覆在 WAN 出現，
  沒有匹配 utun 封包。未綁 source 的額外查詢亦自行選中 fd82 並逾時。
- 決定性 conntrack tuple：原目的 `fd0a:67b1:7933::1:53`，reply source 卻是
  `240e:3b5:d07c:da30::1:7874`，mark 0；client port 53160 雙向都有封包計數。
  Linux 6.12.9 IPv6 REDIRECT 會改目的地址及 port；此現場選中 LAN GUA。
  因此先以 GUA 回覆走 WAN，再由 reverse NAT 還原為 fd0a:53，完整解釋了之前
  capture 與普通 route lookup 的差異。不再保留 TUN／fwmark 或 UDP cache 為此
  已確認鏈路的必要解釋。
- 未改正式路由器設定、route、firewall、RA、lease 或 runtime；PID 3831、配置 SHA
  `40037288…fe2fb` 前後相同，observer 已退出及移除。建議的 `ra_slaac=1`
  修復候選尚未套用或測試，不列 PASS。後續正式授權修復與回驗見 E26。

### E25

2026-10-04 在既有 iStoreOS QEMU 做 `ra_slaac=0→1→0→1` 原生對照，
[報告及原始證據](../.runtime/istoreos-acceptance/20261004-slaac-toggle/report.md)。
router 原生 prefix 為 fd92，獨立 Linux client 由實際 odhcpd RA 的 `/64` PIO
產生 `dynamic proto kernel_ra` 地址；沒有手動加入目標 ULA。

- 候選 1 的 PIO 為 onlink+auto，未綁 source 的 UDP／TCP DNS 與 PTR 成功；
  client／router LAN capture 含完整 request／response，WAN 沒有匹配 DNS packet。
- 回退 0 的 RA 移除 A flag，但 client 已取得的地址保留至 valid lifetime，DNS
  仍能成功；重開 1 後恢復 A。未把手動刪地址當作自動 rollback。
- 初期 client 的 factory netifd 角色及 firewall／RA 接收設定問題保留為環境錯誤，
  修正後回驗；管理 slirp 的 fec0 RA 是模型差異，不是主 router 的 fd92 ULA。
- 本輪 VM DNS oracle 是 stock dnsmasq 的受控本地域名；Smart supplement 未作
  takeover probe，保持 NOT_RUN。沒有全來源刷新／受控 WAN／發布 PASS 宣稱。
  guest sync 後停止，兩個 overlays check 無錯，只刪本輪 client，保留原 router。
  後續正式 Mac／Smart 證據獨立列於 E26，不回填為 QEMU Smart PASS。

### E26

2026-10-04 使用者明示授權測試包含正式路由器與 Mac。正式 `dhcp.lan.ra_slaac`
由 0 改為 1、commit 並僅 reload odhcpd；外部驗證成功後設定保留 1。
[正式修復與原生 Mac 驗收](../.runtime/dns-private-acceptance/20261004-slaac-live/report.md)。
這輪修復是現場 LAN IPv6 設定，不是 Core／LuCI source 或版本變更。

- DHCP UCI export 前後語義僅這一 option 變動，套用前沒有其他 pending changes。
  Mihomo PID 3831、配置 SHA `40037288…fe2fb` 與 Core v0.1.96／LuCI v0.1.0-85
  都維持。未重啟 proxy／network／dnsmasq、改 DNS 上游、路由、lease 或接管規則。
- 原生 Mac 自動產生 `fd0a:67b1:7933:0:dc:b2cc:839d:ffb9/64 autoconf secured`；
  NDP prefix 為 ALO。fd82 仍存在但 deprecated，未由測試程式手動移除。
- Luna High 執行未指定 source 的 14 項 router ULA DNS：UDP 7/7、TCP 7/7
  均為非空 NOERROR，source 全是新 fd0a；Mac 大小寫／.lan A／PTR 與 Notion
  A／AAAA 都符合預期。系統 getaddrinfo 公開及本地各 3/3 成功。
- 代表性 native UDP DNS ID 42085／port 57396 的 request／answer 都在 LAN，
  沒有匹配 WAN／utun 封包。另兩筆 primary UDP／TCP query 的 52696／60642
  conntrack 原目的 fd0a:53、reply GUA:7874，status active lease/path=mihomo。
  NAT 前 GUA source 回新 Mac fd0a 的 lookup 選 br-lan，確認回程缺口已避開。
- E22 的正式 Mac 自動來源 ULA DNS FAIL 已由上述 observed PASS 取代；E22
  保留為歷史失敗。固定綁 fd82／任意第三方 ULA 回程、lease expiry、restore／
  upgrade 與 WAN endpoint 等未驗場景不升格，所以 LUCI-TAKEOVER 整項仍 PARTIAL。
  臨時 observer／backup 已清理，沒有 Git commit 或 release。

<a id="e27"></a>
### E27

2026-10-09 在可拋棄 iStoreOS 24.10.8 x86_64 QEMU，選測 Core v0.1.97
候選 `5ecc27cb839abc4d525dc4ae4c07d425246d3a8a` 的 SITE-ROUTING、MCP-SERVICE、
CONFIG-TEMPLATE、CONFIG-RENDER、CONFIG-VALIDATION 與 CONFIG-APPLY。
[完整報告與原始證據](../.runtime/istoreos-acceptance/20261009-v0197-site-routing/report.md)
保留候選身分、產品操作、首次失敗與限制。

- 同一 qcow2 先以公開 v0.1.96 binary（SHA `34ba6c9e…f3f86a94`）及官方 assets
  正式建立 workspace；由正式路由器只讀同步全部 3 個真實訂閱來源，取得 107 proxies，
  啟動 Meta runtime 後保留 workspace，只替換候選 binary／assets。候選 binary SHA
  `556ed171…0175da`；Meta v1.19.32 SHA `6de24119…73819`。
- 實際 MCP initialize／tools/list 及 `custom_sites_list`／`custom_sites_transact` 通過。
  普通域名與 wildcard 的 active 交易均完成候選驗證、原子提升、hot reload 與 controller
  語義讀回；相同 pattern 的新 proxy 決策優先於舊 direct，刪除後舊決策重新顯現，恢復後
  sequence／順序正確。非法 schema version 保持兩份 durable hash／數量不變；runtime 停止時
  新增回報 pending-next-start，正式啟動後 controller 讀回規則。
- 預設策略同步保留全部 user-owned custom-site 決策與 hash，並加入
  `DOMAIN-SUFFIX,degyax.com,🌍 非中國網站`。候選生成 107 proxies／65 rules，DegYax
  位於 `GEOSITE,cn,DIRECT` 前；Meta isolated config-test 對 SHA
  `8da8d8bf…3b8df` 通過，runtime／controller 亦讀回 DegYax 與自訂網站規則。
- 受控停止 MCP 後，forwarded initialize 以 connection reset 失敗；重啟服務後恢復，
  證明入口 oracle 對服務中斷敏感。首次誤複製 macOS host binary 到 guest 的 syntax error
  已保留，改用明示 Linux amd64 build 後重走候選流程；沒有把錯誤 binary 算作產品結果。
- SITE-ROUTING 記 **PARTIAL**：此 QEMU port-map 沒有獨立 LAN client／受控 endpoint。
  guest-local `degyax.com` DNS 與 HTTPS 200 只作可達性補充，不升格資料面 PASS。
  CONFIG-TEMPLATE 亦因 minimal 未重跑記 **PARTIAL**；LuCI UI、完整 MCP safety matrix
  與 Smart 差異不在本輪變更範圍。
- 候選後續發布為 [v0.1.97](https://github.com/qoli/localClash/releases/tag/v0.1.97)。
  [Release workflow 37910863951](https://github.com/qoli/localClash/actions/runs/37910863951)
  通過；7 項公開資產的 GitHub digest、sidecar、manifest size／SHA 一致，雙架構 binary
  revision 均為 `5ecc27c`。公開 base-assets 已讀回相同 DegYax 模板語義。QEMU 停止、
  qcow2 check 無錯並清理；正式路由器未修改。
