---
name: localclash-istoreos-test
description: Maintain the localClash and localclash-luci feature test table, select regression tests from change impact, reuse verified historical evidence, and assess iStoreOS QEMU release coverage. Use for these repositories' test planning, targeted validation, SOP maintenance, or release acceptance.
---

# localClash 功能追蹤與選測

從本倉庫開始，按 [SOP](../../../docs/istoreos-release-test-sop.md) 處理任務。
先讀 [功能追蹤表](../../../docs/istoreos-test-features.md)，不要先啟動整套 G00–G99。

- 分清定位、針對性修復、一般發版及完整基線。每次改版不等於全表重測。
- 每功能／核心保留最後實測 Core／LuCI 版本、結果與證據。沿用不改成當前版本 PASS；
  最近失敗不可被舊 PASS 隱藏。待核對歷史先找證據，不自動視為未測。
- 本輪另列功能 ID、子斷言、test／reuse 等處置、差異理由及真正前置依賴。
  跨版本沿用比較原實測版本到候選的累積差異；只測直接改動及必要相關回歸。
- 按需要讀 [案例參考](../../../docs/istoreos-test-cases.md)。V 是取證清單，
  A–E/K/F/N/R/X/Z 是操作參考，不是每輪必跑或依章節排序的依賴鏈。
- 選測與派發依 [代理執行規範](../../../docs/istoreos-test-agent-workflow.md)：
  獨立 Pi／Kimi Reviewer、Luna High 執行，主代理核對與整合。文件任務只選相關檢查。
- 只操作已授權的可拋棄 QEMU；環境前置 ERROR、NOT_RUN 與產品 FAIL 分開。
  保留原始 attempt、實際版本及正式入口證據，先驗相關 fixture 再推論產品行為。
- 收尾更新受影響功能與本輪紀錄。一般發版的 G99 結合有效歷史證據與本輪必要重驗；
  針對性任務不替整版放行。不同功能的最後實測版本可以不同。

本 skill 是文件入口，不是自動 QEMU runner／CI gate；不授權產品部署、公開發布或廣播。
