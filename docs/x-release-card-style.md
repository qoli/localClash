# X.com 更新附加圖片樣式

本文件固定 localClash 在 X.com 發佈更新時使用的附加圖片樣式。圖片用途是承載長文字平台不適合直接發布的 changelog 摘要，不是產品介紹圖，也不是 Telegram 固定信息頭。

## 使用場景

- X.com 更新帖需要附加一張 changelog 圖片時使用。
- 內容來源應來自 `docs/changelog.md` 或當次整理出的 release note。
- Telegram 發佈仍使用 `telegram/top.md + telegram/changelog.md` 的文字流程；本圖片只作為 X.com 的視覺補充。

## 內容邊界

只寫 changelog 內容：

- Core 與 LuCI 分開列出。
- 寫版本範圍，例如 `Core v0.1.38`、`LuCI v0.1.0-30`。
- 寫重要變更點，不放完整 commit log。
- 保留維護者和用戶能理解的影響描述。

不要寫：

- 固定 Telegram 信息頭。
- 公開可用、小白體驗、遊戲加速等產品特性介紹。
- 右下角說明性廢話。
- 「完整公告以 Telegram 為準」這類對圖片本身沒有信息量的句子。

## 固定版式

使用深色技術規格卡風格：

- 畫布尺寸：`1600 x 2000` PNG。
- 背景：接近黑色的深色底，外層只保留很弱的徑向光。
- 主卡：深灰黑面板、圓角大卡、細描邊。
- 排版：整張左對齊。
- 頂部為膠囊標記 `RELEASE NOTES`。
- 主標題使用 `Localclash 更新日志`，只允許標題後半段使用藍綠漸變字。
- 標題右側可放短 tag，例如 `(Core + LuCI)`。
- 標題下方只放一句當次 changelog 摘要，概括本輪更新的核心內容，不描述生成流程。
  例：`本次更新補齊 proxy URI 訂閱與來源命名，收緊 MCP 路徑，並完善 LuCI 一鍵更新、cache 沿用與切換前驗證。`
- 接著使用四欄 stats：Core latest、Core range、LuCI latest、LuCI range。
- 主體分為 `CORE CHANGES` 與 `LUCI CHANGES`。
- 每個變更使用小色塊數字枚舉 + 標題 + 一句影響描述；數字按整張卡連續排列。
- 底部不放說明文字；如內容較短，可以保留少量空間，不需要補滿。

## 視覺 token

固定深色技術卡 tokens：

- `--page: #07090d`
- `--panel: #101318`
- `--panel-2: #131821`
- `--line: #2a303a`
- `--line-soft: #1f252e`
- `--text: #f0f3f7`
- `--muted: #9da3af`
- `--dim: #6f7682`
- `--cyan: #6ed7cb`
- `--blue: #78a7ff`
- `--orange: #ffad72`
- `--yellow: #f3cf73`

字體使用 system sans stack，優先：

```css
Inter, ui-sans-serif, -apple-system, BlinkMacSystemFont, "SF Pro Display",
"SF Pro Text", "PingFang TC", "PingFang SC", "Microsoft JhengHei", sans-serif
```

不要使用紙面米色風格、serif 主標題、產品介紹卡、居中海報標語、右下角說明性廢話、內部 artifacts 列表或裝飾性 blob。

## 輸出文件

當次生成文件放在 ignored 目錄：

- HTML: `telegram/out/localclash-x-release-card.html`
- PNG: `telegram/out/localclash-x-release-card.png`

`telegram/out/` 必須保持不進 Git。只提交本規範、release 文檔或腳本變更。

## 渲染與檢查

優先使用本機 Arc CDP 截圖，避免啟動獨立 Playwright 瀏覽器：

```bash
python3 -m unittest scripts/test_release_broadcast.py
curl -s http://localhost:9222/json/version
python3 - <<'PY'
from pathlib import Path
from playwright.sync_api import sync_playwright

html = Path('telegram/out/localclash-x-release-card.html').resolve().as_uri()
out = Path('telegram/out/localclash-x-release-card.png').resolve()
with sync_playwright() as p:
    browser = p.chromium.connect_over_cdp('http://localhost:9222')
    if not browser.contexts:
        raise RuntimeError('Arc CDP has no existing browser context')
    context = browser.contexts[0]
    page = context.new_page()
    try:
        page.set_viewport_size({'width': 1600, 'height': 2000})
        page.goto(html, wait_until='networkidle')
        page.screenshot(path=str(out), full_page=False)
    finally:
        page.close()
PY
```

不要在連接 Arc CDP 後呼叫 `browser.new_context()`。Arc 會把隔離 context
映射成獨立窗口，受影響版本可能留下無法關閉的窗口。也不要對連接中的 Arc
呼叫 `browser.close()`。必須重用 `browser.contexts[0]`，只建立一個暫時 tab，
截圖後只關閉該 tab；如果沒有既有 context，就明確失敗。

同一日期同時包含 Core 與 LuCI，但本次只公告其中一個渠道時，使用
`scripts/x-release-card.py --channel core` 或 `--channel luci`。不要把另一個已
公告渠道的內容塞入圖片，否則固定高度卡片可能溢出或造成公告範圍誤導。

生成後檢查：

```bash
file telegram/out/localclash-x-release-card.png
python3 - <<'PY'
from PIL import Image
print(Image.open('telegram/out/localclash-x-release-card.png').size)
PY
git check-ignore -v telegram/out/localclash-x-release-card.html telegram/out/localclash-x-release-card.png
```

人工檢查重點：

- 是否只包含 changelog。
- 是否全局左對齊。
- 是否沒有右下角說明文字。
- 是否沒有文字裁切、重疊或底部壓線。
