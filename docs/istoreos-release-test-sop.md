# iStoreOS 功能驗收 SOP

以 [功能測試表](istoreos-test-features.md)定義產品能力及保存最後實測結果。
本文件定義如何取得足以發現未知串接故障的功能證據；代理執行方式由
[測試 skill](../.codex/skills/localclash-istoreos-test/SKILL.md)維護。

## 驗收原則

功能驗收以使用者從產品外部可觀察的結果為準，不以產品自己回報的 readiness 代替。
PID、HTTP 200、RPC 成功、task 完成、controller ready、狀態 JSON、配置檔、nft 規則、
lease 或資產存在，都只能解釋結果，不能單獨構成 PASS。

主機端 mock、fake、stub、靜態 grep、單元測試及 CI 可以檢查純函式、錯誤傳遞、格式、
建置及交易邊界，但不得列入 iStoreOS 功能驗收證據，也不得提高功能表的實測版本或結果。
受控外部服務可以提供訂閱、下載、DNS 回應及網路 endpoint；不得替換被驗收的 localClash、
LuCI、rpcd、Mihomo、dnsmasq、procd、fw4、nft、iproute 或產品 helper。

## 測試模型

每個功能列都必須先寫成可執行契約，至少包含：

1. 前置產品狀態及由正式入口建立狀態的方法。
2. 使用者操作或真實事件。
3. 與被測元件獨立的結果 oracle，例如另一台 LAN client 的回應、實際出口 endpoint、
   重新開頁後的持久資料，或重啟後重新讀回的產品行為。
4. 功能宣稱包含的狀態轉移，例如首次執行、重複執行、停止、恢復、更新或重啟。
5. 失敗後應保持或恢復的使用者行為。只有功能本身宣稱管理該失敗時才列為必要轉移。

不窮舉所有硬件、網路供應商或配置排列。按產品支援契約劃分等價類，至少採用產品預設、
支援的舊版升級狀態、目前正式使用方式及變更涉及的輸入類別；用狀態轉移及組合覆蓋選例。
新 Bug 的專用回歸只能防止重現，不能取代上述功能契約或被稱為完整驗收。

## QEMU 環境

功能交互一律使用本輪授權的可拋棄 iStoreOS QEMU，透過正式 LuCI／rpcd／Core 入口操作。
[VM 腳本](../scripts/istoreos-test-env.sh)只管理 VM。涉及路由、DNS 或接管時，使用已打通的
非透明代理 WAN，並建立獨立 router、LAN client 與受控 WAN endpoint；從 client 發出真實
TCP、UDP、DNS 或應用請求，按功能契約在 endpoint 或回應端核對結果。

網路功能在正常、停止、恢復及所選故障轉移後都要重新跑相同資料面 oracle。內部狀態、
log、counter、packet capture 只用於解釋外部成功或失敗。若外部 oracle 沒有執行，該資料面
結果就是 NOT_RUN；若 oracle 失敗，即使內部狀態全部顯示正常仍是 FAIL。

驗收前先在同一可拋棄環境破壞一項必要路徑，確認 oracle 確實會失敗，再恢復候選進行正式
操作。這是測試敏感度檢查，不得把破壞後的預期失敗冒充產品負向能力 PASS。

## 執行

1. 比較候選與基準，列出受影響功能、共享依賴及必要交互；完整功能核對則展開功能表全部
   契約及適用狀態轉移，不以「每列執行過一個命令」代替完整覆蓋。
2. 記錄 Core／LuCI commit、package 與 dependency SHA、iStoreOS image、Mihomo flavor／SHA、
   QEMU 拓撲、輸入等價類、正式入口、oracle、狀態轉移及沿用理由。
3. 從正式入口建立前置狀態並執行使用者流程；UI 能力走真 UI。每次狀態轉移後先判斷外部
   結果，再收集任務、runtime、接管、配置及日誌證據作解釋。
4. 保留首次失敗。診斷及修復不得改寫該 attempt；修復後用相同 oracle 重走原流程。
   環境問題只影響真正依賴它的測試，其他功能繼續。
5. 回寫真正實測的功能、狀態轉移、等價類、結果及證據。沿用維持原版本；局部通過不能
   升格整列 PASS，後來觀察到的同功能失敗必須撤回或取代舊 PASS。

## 結果與發布

- PASS：正式產品入口及全部本輪必要狀態轉移均通過獨立外部 oracle。
- FAIL：正常產品操作或外部 oracle 違反契約；內部 readiness 不能覆蓋此結果。
- PARTIAL：同一功能只有部分明列契約取得有效結果，其餘逐項記 FAIL、ERROR 或 NOT_RUN。
- ERROR：測試基礎設施無法提供原計畫所需條件，且沒有取得產品結果。
- NOT_RUN：必要 oracle 或狀態轉移未執行。

原始證據放在 `.runtime/istoreos-acceptance/<run-id>/`，版控只記脫敏摘要與索引。
發布摘要（沿用 G99 名稱）列出候選、實測與沿用契約、首次失敗、修復回驗、NOT_RUN、
剩餘風險及證據入口。CI、建置、資產校驗及 mock 測試永遠不能替代 QEMU 功能驗收；
公開發布仍需使用者另行授權。
