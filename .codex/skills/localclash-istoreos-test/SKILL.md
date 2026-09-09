---
name: localclash-istoreos-test
description: Select and execute affected localClash and LuCI tests, and maintain each feature's last tested version and evidence in the iStoreOS feature table.
---

# localClash 功能測試

讀取 [功能測試表](../../../docs/istoreos-test-features.md)，以它作為功能範圍與實測歷史的唯一依據。

- 主代理按本輪變更選定功能、共享依賴、必要狀態轉移及獨立外部 oracle，說明範圍及沿用理由；需要沿用時核對原實測版本至候選的累積差異。不要求全表或所有功能雙核心重測。
- 測試交由子代理執行，明確指定 `model: gpt-5.6-luna`、`reasoning_effort: high`。主代理提供候選身分、所選功能、授權範圍、環境與證據位置，核對結果並統一回寫；子代理不再派代理。無需第三方選測審查。
- 功能測試使用本輪授權的可拋棄 iStoreOS QEMU，走真實入口並以產品外部可觀察結果判定；涉及網路時使用獨立 LAN client、非透明代理 WAN 及受控 endpoint。mock、fake、stub、內部 readiness、單元測試或 CI 不得作功能 PASS 證據。只準備所選功能需要的環境；文件變更只做相關文件檢查。
- 保留實際版本／SHA、核心、操作、結果及原始證據；產品 FAIL 與環境 ERROR／NOT_RUN 分開。發現缺陷後依授權修復並回驗，獨立功能繼續執行。
- 回寫受測功能的最後實測版本、等價類、狀態轉移、外部 oracle 與具體覆蓋；沿用保留原版本，部分通過不升格整項 PASS。後來發現的同功能失敗須撤回或取代舊 PASS。收尾說明測過什麼、問題與回驗、未完成項及剩餘風險。
