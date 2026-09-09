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
- **LuCI**：另列 OpenWrt 包裝、UI 任務、更新編排、DNS 最佳化整合及網路接管。
  接管可用受控請求證明整合生效，不展開為 TCP／UDP／IPv6 傳輸能力矩陣。
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
| SUB-REFRESH | **訂閱取得與刷新**：取得並解析多來源、合併節點及重建已配置的服務能力篩選材料（如 ChatGPT 可用節點）；來源失敗如實呈現，有有效來源與全部無效的結果不同；失敗保留合法材料。保存來源與刷新生效分別讀回。[實作入口](../product_cli.go) | Core／共用 | v0.1.83／不適用；PASS；多來源合併、部分失敗、全失敗、合法材料恢復及能力快照 [E05](#e05) |
| SUB-NODES | **節點查詢**：列出與搜尋已取得節點，名稱／類型與來源一致，不洩漏連線憑證；查詢結果不冒充節點品質或出口地理驗證。[實作入口](../internal/mcp/registry.go) | Core／共用 | v0.1.83／不適用；PASS；MCP 列出／搜尋、來源欄位及敏感資料界線 [E05](#e05) |

<a id="sites"></a>
## 網站與路由設定

| 功能 ID | 功能／驗證內容 | 責任／核心範圍 | 最後實測版本；結果；證據 |
| --- | --- | --- | --- |
| SITE-ROUTING | **自訂網站分流**：新增、刪除普通域名與萬用字元，設定直連／代理並重新開頁；保存、生成規則、熱載入及 controller 讀回一致；合法 DomainSuffix 不被誤判回滾。另驗衝突順序及非法輸入不破壞既有設定。[實作入口](../internal/customsitesapply/transaction.go) | Core；LuCI 提供頁面／共用 | v0.1.83／0.1.0-76；PASS；UI 與 Core 增刪／重開、DomainSuffix、衝突順序、熱載入、controller 及非法輸入保留 [E05](#e05) |
| RULE-PACKS | **規則包查詢與材料取得**：搜尋目錄、查看指定規則包、預取及查詢其規則；來源／類型／快取缺口如實顯示，目錄建議出口不當成已啟用設定。[實作入口](../internal/mcp/registry.go) | Core／共用 | v0.1.83／不適用；PASS；目錄搜尋／查看／預取／讀取／查詢、來源型別、快取缺口及建議出口界線 [E05](#e05) |
| RULE-CUSTOM | **自訂規則與外部規則來源**：建立域名、CIDR、GEOIP 規則及外部 rule-provider 設定，經配置補丁保存並生成；拒絕非法值或無效引用，順序與目標正確。[實作入口](../internal/mcp/registry.go) | Core／共用 | v0.1.83／不適用；PASS；Domain／CIDR／GEOIP／provider 生成、順序與目標、非法值／引用拒絕 [E05](#e05) |
| PROXY-GROUPS | **代理群組設定**：由精確節點或 selector 建立群組，經補丁保存／修改／移除；查詢解析結果及生成成員正確，builder 預覽不冒充已保存或已載入。[實作入口](../internal/mcp/registry.go) | Core／共用 | v0.1.83／不適用；PASS；精確節點／selector builder、保存／修改／移除、解析成員及預覽界線 [E05](#e05) |
| POLICY-GROUPS | **業務策略群組設定**：把網站／應用／規則包對應到現有出口，經補丁保存及生成；引用與優先順序正確，不意外改動其他群組；不驗 Mihomo 自動選路品質。[實作入口](../internal/mcp/registry.go) | Core／共用 | v0.1.83／不適用；PASS；網站／應用／規則包對應、引用／順序及其他群組保留 [E05](#e05) |

<a id="config"></a>
## 配置管理

| 功能 ID | 功能／驗證內容 | 責任／核心範圍 | 最後實測版本；結果；證據 |
| --- | --- | --- | --- |
| CONFIG-TEMPLATE | **策略模板設定**：選取完整預設或 minimal 模板、配置 profile，產生相應補丁及 intent；重設模板只依明示選項處理既有補丁，不默默覆蓋自訂設定。[實作入口](../product_cli.go) | Core／共用 | v0.1.83／不適用；PASS；default／minimal、normal／router、Meta／Smart 及 normal reset 保留自訂 patch [E05](#e05) |
| CONFIG-PATCHES | **配置補丁管理**：讀取、預覽、套用、移除、啟停及排序補丁；預覽不落盤，套用後 registry／intent 一致，失效草稿或非法引用明確拒絕。[實作入口](../product_cli.go) | Core／共用 | v0.1.83／不適用；PASS；預覽／套用／移除／啟停／排序／tombstone、registry 恢復及非法／過期草稿拒絕 [E05](#e05) |
| CONFIG-RENDER | **Mihomo 配置生成**：由訂閱、模板、補丁與 profile 生成正確配置。Meta 自動組為 url-test，Smart 為 smart 並移除 tolerance；Smart 參數、群組 priority 與 defaults 按 intent 傳遞且不覆蓋既有值；生成不等於已載入。[實作入口](../product_cli.go) | Core／按核心差異 | v0.1.83／不適用；PASS；Meta url-test、Smart smart／參數／priority／defaults 及生成未載入界線 [E05](#e05) |
| CONFIG-VALIDATION | **配置驗證**：用所選核心驗證生成配置，記錄對應 hash；非法配置不取得通過證明；驗證與活躍程序隔離，不爭用工作目錄。[實作入口](../product_cli.go) | Core／按核心差異 | v0.1.83／不適用；PASS；Meta／Smart hash 證明、非法 YAML 不改 attestation、活躍程序隔離 [E05](#e05) |
| CONFIG-APPLY | **配置套用**：按所選入口核對候選檔提交或 runtime 載入；config-promote 是其中一個檔案提交入口，不是所有流程的前置。驗證 hash、實際載入規則／組及失敗結果符合契約，提交檔案不當成已熱載入，生成檔存在不當成已生效。[實作入口](../product_cli.go) | Core／按核心差異 | v0.1.83／不適用；PASS；非法候選拒絕、合法原子提交、hash／controller 讀回及 restart 前後載入界線 [E05](#e05) |

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
| COMPONENT-MIHOMO | **Mihomo 核心取得與更新**：依平台／架構取得選定來源的兩份核心，核對版本／SHA；成對更新與失敗恢復一致，preflight 使用活躍核心，更新後保留選擇並正常啟動。[實作入口](../product_mihomo_update.go) | Core／共用交易，按核心差異驗啟動 | v0.1.83／不適用；PASS；精確 Meta／Smart 成對更新、SHA、活躍核心 preflight、殘留 rollback 拒絕與 hash 保留 [E05](#e05) |
| COMPONENT-ASSETS | **基礎資源更新**：取得並安裝基礎資源，版本／完整性正確；更新失敗不假報完成，之後配置生成可使用實際安裝的資源。[實作入口](../product_cli.go) | Core／共用 | v0.1.83／不適用；PASS；正式安裝、配置使用、錯誤 manifest／損壞 archive 拒絕及原 hash 保留 [E05](#e05) |
| COMPONENT-DASHBOARD | **Dashboard 資源管理**：取得、更新及提供面板資源，檔案／入口與 controller 連接設定一致；能開啟面板，不驗 Dashboard 自身全部功能。[實作入口](../product_cli.go) | Core；LuCI 提供連結／共用 | Core `11ee263`／LuCI `2ac337f`；PASS；有效 archive 更新、component status、`external-ui`、controller `/ui/`，以及 5 次失敗後 optional skip／原 hash 保留 [E07](#e07) |

<a id="access"></a>
## 狀態與診斷

| 功能 ID | 功能／驗證內容 | 責任／核心範圍 | 最後實測版本；結果；證據 |
| --- | --- | --- | --- |
| STATUS-INSPECT | **產品狀態查詢**：查配置、元件、訂閱及 runtime facts，未初始化／停止／運行／錯誤狀態與實際資料一致；唯讀不改狀態，版本化 facts 不把宿主接管當 Core 所有。[實作入口](../internal/mcp/registry.go) | Core／共用 | v0.1.83／不適用；PASS；未初始化／停止／運行／錯誤、元件／訂閱／runtime facts、唯讀 hash 與接管責任界線 [E05](#e05) |
| DIAG-ROUTING | **路由設定解釋**：按域名、服務或出口查詢編譯 intent 的規則、群組及來源；能對照自訂網站及補丁變更，不把 intent 解釋當活躍流量證據。[實作入口](../internal/mcp/registry.go) | Core／共用 | v0.1.83／不適用；PASS；domain／service／exit 查詢、自訂規則／patch provenance 及 intent／流量證據界線 [E05](#e05) |
| DIAG-HEALTH | **診斷與日誌取得**：執行 doctor／環境檢查、收集產品日誌及讀取受限 controller 日誌／連線；診斷可定位問題、讀取有界且不洩漏秘密，不由缺少連線推論未來路由。[實作入口](../internal/mcp/registry.go) | Core／共用 | v0.1.83／不適用；PASS；doctor、受限日誌／controller／connections、redaction 與空連線推論界線 [E05](#e05) |
| MCP-SERVICE | **MCP 服務與工具存取**：連接正確服務並完成協定初始化、工具發現與呼叫；工具結果／錯誤／權限符合入口契約。檔案讀改與 controller 存取受限定，不把 MCP 當任意路徑或任意 URL 代理。[實作入口](../internal/mcp/registry.go) | Core；LuCI 管理 procd／共用 | v0.1.83／0.1.0-76；PASS；協定初始化／發現／呼叫／服務啟停、controller allowlist、URL／路徑 traversal／寫入拒絕 [E05](#e05) |

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
| LUCI-INSTALL | **OpenWrt 安裝與版本管理**：離線包安裝、重裝、LuCI／Core 安裝更新及支援的舊版交接正常；架構／完整性錯誤拒絕，應保留資料不丟失。Core 自我更新未實作，實際由 helper 管理。[實作入口](../../localclash-luci/openwrt/luci-app-localclash/root/usr/libexec/rpcd/localclash) | LuCI／共用 | Core `11ee263`／LuCI `2ac337f`；PASS；離線安裝／重裝、受控 76→77 helper 更新、舊版本拒絕、checksum／套件名／arm64 預檢及資料保留 [E07](#e07) |
| LUCI-INIT | **初始化引導**：由頁面提供訂閱、模板及核心，完成 Core 呼叫、配置、啟動及接管；重新開頁顯示真實狀態，失敗可定位。[實作入口](../../localclash-luci/openwrt/luci-app-localclash/root/usr/libexec/rpcd/localclash) | LuCI 編排＋Core／按影響 | v0.1.83／0.1.0-76；PASS；空工作區 Meta／Smart 初始化、訂閱／模板／Core、UI 啟動並接管、重開讀回及可定位失敗後恢復 [E05](#e05) |
| LUCI-UPDATE | **一鍵更新與資料保留**：從頁面更新，兩個檢查點、來源版本及結果可讀回；重跑／舊版升級保持訂閱、網站順序、偏好及核心選擇；原本停止或未初始化的狀態不擅自啟動。[實作入口](../../localclash-luci/openwrt/luci-app-localclash/root/usr/libexec/rpcd/localclash) | LuCI 編排＋Core／按影響 | v0.1.83／0.1.0-76；PASS；UI／受控 76→77、重跑、software／material checkpoints、資料／選擇保留、停止／未初始化不自啟及 76 恢復 [E05](#e05) |
| LUCI-TASKS | **介面與長任務交互**：訂閱、網站、初始化及更新頁面操作與後端一致；日誌、取消、互斥、重新連接與終態可用，無重複交易／無限 busy；依各操作是否支援取消驗證。[實作入口](../../localclash-luci/openwrt/luci-app-localclash/root/usr/libexec/rpcd/localclash) | LuCI／共用 | v0.1.83／0.1.0-76；PASS；訂閱／網站／初始化／更新頁、日誌、代表性取消／互斥、同 task id reload／reopen 及終態恢復 [E05](#e05) |
| LUCI-DNS | **DNS 最佳化設定整合**：查詢、量測、套用及移除 dnsqualify 結果；資格／WAN 身分不符明確拒絕，傳入配置正確，移除後恢復基線。不評比外部解析器或驗 dnsqualify 演算法。[實作入口](../../localclash-luci/openwrt/luci-app-localclash/root/usr/libexec/rpcd/localclash) | LuCI 編排＋Core 配置／共用 | v0.1.83／0.1.0-76；PASS；真實量測無合格結果界線、受控 qualified 套用／讀回／移除、config-test、WAN eth0→eth9 拒絕及 hash rollback [E05](#e05) |
| LUCI-TAKEOVER | **OpenWrt 網路接管**：套用及停止本產品的防火牆、策略路由、DNS 接管，保留非本產品規則；使用受控請求驗設定／接管整合，停止後恢復基線。不是傳輸協定能力矩陣。[實作入口](../../localclash-luci/openwrt/luci-app-localclash/root/usr/libexec/rpcd/localclash) | LuCI／共用 | v0.1.83／0.1.0-76；PASS；VDE 真 LAN client 192.168.101.10→endpoint 192.0.2.3、apply／stop、fw4／nft／policy／DNS 狀態及非產品 marker 保留 [E05](#e05) |
| LUCI-RESTORE | **接管及服務恢復**：開機恢復偏好、WAN 事件、受管程序退出與 DNS 健康租約，按意圖恢復或撤回接管；明確停止後不自行重開。只驗 LuCI 恢復與 Core 正常啟動。[實作入口](../../localclash-luci/openwrt/luci-app-localclash/root/usr/libexec/rpcd/localclash) | LuCI＋Core 生命週期／共用 | v0.1.83／0.1.0-76；PASS；QEMU system_reset 開機恢復、WAN／TUN 事件、受管程序退出／恢復、DNS lease grant／retract 及明確停止不重開 [E05](#e05) |

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

2026-09-08 對功能表 31 項能力執行完整候選核對。主要證據為
[R4 Core／MCP 報告](../.runtime/istoreos-acceptance/20260908-full-functional-v0183-v076-r4/execution-report.md)、
[R5 官方身分／LuCI UI 報告](../.runtime/istoreos-acceptance/20260908-full-functional-v0183-v076-r5/execution-report.md)、
[R6 元件／工作區／恢復報告](../.runtime/istoreos-acceptance/20260908-full-functional-v0183-v076-r6/execution-report.md)、
[R7 LuCI 缺口報告](../.runtime/istoreos-acceptance/20260908-full-functional-v0183-v076-r7/execution-report.md)及
[R8 VDE／DNS 報告](../.runtime/istoreos-acceptance/20260908-full-functional-v0183-v076-r8/r8-report.md)。
實測入口包括 Core CLI、MCP、rpcd／ubus、真實 LuCI 頁面及三台 QEMU 的 VDE LAN；
結果為 28 項 PASS、1 項 FAIL、2 項部分通過。

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
`index.js` 的元件、服務、dnsqualify、`reset` RPC 是本表入口依據。
E05 已按候選版本核對這些正式入口及真實 UI；目前不新增健康 S2 運行中跨核心切換、
配置單獨 reset、DNS 資格到期自動處理等不存在的 UI 功能。

DNS health lease 是已落地的獨立接管能力：E05 在 QEMU 實測 lease grant／retract、
受管程序退出及開機／事件恢復。它使用 nft
`flags timeout` 集合及 WAN/dnsmasq 基線，與 DNS 最佳化資格到期是不同契約。
既有來源碼及 mock 測試內容只用來補齊功能定義，不替代本輪 QEMU 證據。

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
