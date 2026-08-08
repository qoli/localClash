#!/usr/bin/env python3
"""Generate the X.com changelog card from docs/changelog.md."""

from __future__ import annotations

import argparse
import html
import re
import sys
from dataclasses import dataclass
from pathlib import Path


CARD_WIDTH = 1600
CARD_HEIGHT = 2000
DEFAULT_CDP_URL = "http://localhost:9222"
DEFAULT_HTML_NAME = "localclash-x-release-card.html"
DEFAULT_PNG_NAME = "localclash-x-release-card.png"


class ScriptError(RuntimeError):
    pass


@dataclass
class ChangeItem:
    channel: str
    title: str
    body: str
    color: str


@dataclass
class CardData:
    core_latest: str
    core_range: str
    core_range_hint: str
    luci_latest: str
    luci_range: str
    luci_range_hint: str
    summary: str
    core_items: list[ChangeItem]
    luci_items: list[ChangeItem]


def read_text(path: Path) -> str:
    return path.read_text(encoding="utf-8")


def latest_dated_section(markdown: str) -> tuple[str, str]:
    matches = list(re.finditer(r"^## (\d{4}-\d{2}-\d{2})\s*$", markdown, re.MULTILINE))
    if not matches:
        raise ScriptError("No dated changelog section found.")
    match = matches[0]
    start = match.end()
    next_match = re.search(r"^## \S+", markdown[start:], re.MULTILINE)
    end = start + next_match.start() if next_match else len(markdown)
    return match.group(1), markdown[start:end].strip()


def split_release_blocks(section: str) -> list[tuple[str, str]]:
    headings = list(re.finditer(r"^### (.+?)\s*$", section, re.MULTILINE))
    blocks: list[tuple[str, str]] = []
    for idx, heading in enumerate(headings):
        start = heading.end()
        end = headings[idx + 1].start() if idx + 1 < len(headings) else len(section)
        blocks.append((heading.group(1).strip(), section[start:end].strip()))
    return blocks


def join_wrapped_line(current: str, continuation: str) -> str:
    continuation = continuation.strip()
    if not current:
        return continuation
    if not continuation:
        return current
    if should_join_with_space(current[-1], continuation[0]):
        return current + " " + continuation
    return current + continuation


def should_join_with_space(left: str, right: str) -> bool:
    if left in "([{`，。；：、" or right in ".,;:!?，。；：、）)]}":
        return False
    return left.isascii() or right.isascii()


def extract_changes(block: str) -> list[str]:
    match = re.search(r"^Changes:\s*$", block, re.MULTILINE)
    if not match:
        return []
    tail = block[match.end() :]
    next_section = re.search(r"^[A-Z][A-Za-z ]+:\s*$", tail, re.MULTILINE)
    if next_section:
        tail = tail[: next_section.start()]

    changes: list[str] = []
    current = ""
    for raw_line in tail.splitlines():
        line = raw_line.rstrip()
        if not line.strip():
            continue
        if line.lstrip().startswith("- "):
            if current:
                changes.append(current.strip())
            current = line.strip()[2:].strip()
            continue
        if current:
            current = join_wrapped_line(current, line)
    if current:
        changes.append(current.strip())
    return changes


def strip_markdown(text: str) -> str:
    text = re.sub(r"\[([^\]]+)\]\(([^)]+)\)", r"\1", text)
    text = text.replace("`", "")
    text = text.replace("**", "")
    return re.sub(r"\s+", " ", text).strip()


def parse_latest_versions(markdown: str) -> tuple[str, str]:
    core = ""
    luci = ""
    for line in markdown.splitlines():
        if line.startswith("| localClash Core |"):
            core = extract_version_from_table_line(line)
        elif line.startswith("| localclash-luci |"):
            luci = extract_version_from_table_line(line)
    if not core or not luci:
        raise ScriptError("Cannot parse latest Core/LuCI versions from docs/changelog.md.")
    return core, luci


def extract_version_from_table_line(line: str) -> str:
    cells = [cell.strip() for cell in line.strip("|").split("|")]
    if len(cells) < 2:
        return ""
    match = re.search(r"\[([^\]]+)\]", cells[1])
    return match.group(1) if match else cells[1]


def release_range_from_title(title: str, channel: str) -> str:
    cleaned = title
    cleaned = re.sub(r"^localClash Core\s+", "", cleaned, flags=re.IGNORECASE)
    cleaned = re.sub(r"^localclash-luci\s+", "", cleaned, flags=re.IGNORECASE)
    if channel == "core":
        cleaned = cleaned.replace("v", "")
        return cleaned.strip()
    return cleaned.replace("v0.1.0-", "").replace("v", "").strip()


def stat_value_and_hint(range_text: str, latest: str, channel: str) -> tuple[str, str]:
    if channel == "core":
        if "," in range_text:
            first, rest = [part.strip() for part in range_text.split(",", 1)]
            return f"{first}+", rest.replace(" 到 ", "-")
        return latest, range_text
    if "-" in range_text:
        return range_text, "integrated"
    return latest, "latest"


def skip_change(text: str) -> bool:
    stripped = strip_markdown(text)
    skip_markers = [
        "自 2026-",
        "本次整合",
        "可公開使用的小白流程",
        "不需要先理解 mihomo YAML",
    ]
    return any(marker in stripped for marker in skip_markers)


def items_from_change(channel: str, change: str) -> list[ChangeItem]:
    text = strip_markdown(change)
    if skip_change(text):
        return []

    if channel == "core" and "非 URI 說明行" in text:
        return [ChangeItem(channel, "容忍訂閱說明行", "機場輸出的 REMARKS 與 STATUS 不再阻斷有效 proxy URI。", "cyan")]
    if channel == "luci" and "mirror fallback log" in text:
        return [ChangeItem(channel, "鏡像下載日誌", "直接下載失敗、鏡像候選與後續嘗試更容易追蹤。", "blue")]

    if channel == "core" and "proxy URI" in text and "display_name" in text:
        return [
            ChangeItem(channel, "Proxy URI 訂閱", "新增 proxy URI 訂閱來源支援，多來源訂閱輸入更完整。", "cyan"),
            ChangeItem(channel, "來源命名更清楚", "節點名前綴優先使用 display_name，多個訂閱來源更容易辨識。", "blue"),
        ]
    if channel == "core" and "server-owned" in text and "amd64-v1" in text:
        return [
            ChangeItem(
                channel,
                "MCP 路徑收緊",
                "MCP 工具不再接收 caller 傳入的 server-owned config / runtime / core 路徑。",
                "orange",
            ),
            ChangeItem(channel, "amd64 資產保守化", "Mihomo core 改用更保守的 amd64-v1 資產，降低老設備兼容風險。", "yellow"),
        ]
    if channel == "luci" and any(
        marker in text
        for marker in [
            "新版 helper 同 PID 接棒",
            "Core 替換後強制 service restart",
            "服務生命週期失敗顯式化",
            "舊版首次升級需兩步",
        ]
    ):
        return [fallback_item(channel, text)]
    if channel == "luci" and "概覽頁重做為摘要表格" in text:
        return [
            ChangeItem(channel, "概覽頁更新檢查", "概覽頁重做為摘要表格，背景檢查 LuCI / Core 更新；進階頁保留組件級維護。", "blue")
        ]
    if channel == "luci" and "一鍵更新" in text:
        return [
            ChangeItem(
                channel,
                "一鍵更新鏈路",
                "串起 LuCI、Core、Mihomo、Dashboard、訂閱刷新、配置重建、MCP 檢查與接管恢復。",
                "cyan",
            )
        ]
    if channel == "luci" and "merged subscription cache" in text:
        return [
            ChangeItem(channel, "訂閱失敗處理", "刷新失敗時可明確沿用既有 merged subscription cache。", "orange"),
            ChangeItem(channel, "切換前驗證", "即使沿用 cache，仍必須通過 config render 與 mihomo config-test 才會切換 runtime。", "yellow"),
        ]

    return [fallback_item(channel, text)]


def fallback_item(channel: str, text: str) -> ChangeItem:
    color = "cyan" if channel == "core" else "blue"
    if "：" in text:
        title, body = text.split("：", 1)
    elif "，" in text:
        title, body = text.split("，", 1)
    else:
        title, body = "更新項目", text
    return ChangeItem(channel, title.strip(" ，。；："), body.strip(" ，。；："), color)


def build_summary(items: list[ChangeItem]) -> str:
    titles = {item.title for item in items}
    if {"Proxy URI 訂閱", "來源命名更清楚", "MCP 路徑收緊", "一鍵更新鏈路"}.issubset(titles):
        return "本次更新補齊 proxy URI 訂閱與來源命名，收緊 MCP 路徑，並完善 LuCI 一鍵更新、cache 沿用與切換前驗證。"
    visible = [item.title for item in items[:4]]
    if not visible:
        raise ScriptError("No changelog items available for the X.com release card.")
    return "本次更新聚焦" + "、".join(visible) + "。"


def build_card_data(changelog: str) -> CardData:
    core_latest, luci_latest = parse_latest_versions(changelog)
    _, section = latest_dated_section(changelog)
    blocks = split_release_blocks(section)
    if not blocks:
        raise ScriptError("No release blocks found in latest changelog section.")

    core_range = core_latest
    luci_range = luci_latest
    core_range_seen = False
    luci_range_seen = False
    core_items: list[ChangeItem] = []
    luci_items: list[ChangeItem] = []

    for title, block in blocks:
        channel = channel_from_title(title)
        if channel is None:
            continue
        range_text = release_range_from_title(title, channel)
        if channel == "core" and not core_range_seen:
            core_range = range_text
            core_range_seen = True
        elif channel == "luci" and not luci_range_seen:
            luci_range = range_text
            luci_range_seen = True
        elif channel == "core" or channel == "luci":
            continue
        for change in extract_changes(block):
            parsed = items_from_change(channel, change)
            if channel == "core":
                core_items.extend(parsed)
            else:
                luci_items.extend(parsed)

    if not core_items and not luci_items:
        raise ScriptError("No user-facing changelog items parsed for the X.com release card.")

    core_value, core_hint = stat_value_and_hint(core_range, core_latest, "core")
    luci_value, luci_hint = stat_value_and_hint(luci_range, luci_latest, "luci")
    all_items = (core_items + luci_items)[:8]
    summary = build_summary(all_items)
    return CardData(
        core_latest=core_latest,
        core_range=core_value,
        core_range_hint=core_hint,
        luci_latest=luci_latest,
        luci_range=luci_value,
        luci_range_hint=luci_hint,
        summary=summary,
        core_items=core_items[:4],
        luci_items=luci_items[:4],
    )


def channel_from_title(title: str) -> str | None:
    lowered = title.lower()
    if "localclash core" in lowered:
        return "core"
    if "localclash-luci" in lowered:
        return "luci"
    return None


def emph(text: str) -> str:
    escaped = html.escape(text)
    for token in ["Core", "LuCI", "proxy URI", "display_name", "amd64-v1", "cache"]:
        escaped = escaped.replace(html.escape(token), f"<strong>{html.escape(token)}</strong>")
    return escaped


def render_change(item: ChangeItem, number: int) -> str:
    return f"""
          <div class=\"change\">
            <div class=\"icon {html.escape(item.color)}\" data-num=\"{number}\"></div>
            <div>
              <h2>{html.escape(item.title)}</h2>
              <p>{emph(item.body)}</p>
            </div>
          </div>"""


def render_html(data: CardData) -> str:
    core_html = "\n".join(render_change(item, idx + 1) for idx, item in enumerate(data.core_items))
    luci_html = "\n".join(render_change(item, idx + 1 + len(data.core_items)) for idx, item in enumerate(data.luci_items))
    core_section = ""
    if core_html:
        core_section = f"""
      <section class=\"section\">
        <div class=\"section-label\">Core Changes</div>
        <div class=\"changes\">{core_html}
        </div>
      </section>"""
    luci_section = ""
    if luci_html:
        luci_section = f"""
      <section class=\"section\">
        <div class=\"section-label\">LuCI Changes</div>
        <div class=\"changes\">{luci_html}
        </div>
      </section>"""
    released_channels = " + ".join(
        label for label, items in (("Core", data.core_items), ("LuCI", data.luci_items)) if items
    )
    return f"""<!DOCTYPE html>
<html lang=\"zh-Hant\">
<head>
<meta charset=\"UTF-8\">
<meta name=\"viewport\" content=\"width={CARD_WIDTH}, initial-scale=1.0\">
<title>Localclash 更新日志</title>
<style>
  :root {{
    --page: #07090d;
    --panel: #101318;
    --line: #2a303a;
    --text: #f0f3f7;
    --muted: #9da3af;
    --dim: #6f7682;
    --cyan: #6ed7cb;
    --blue: #78a7ff;
    --orange: #ffad72;
    --yellow: #f3cf73;
    --green-bg: #17342f;
    --blue-bg: #17233a;
    --orange-bg: #30211d;
    --yellow-bg: #2b2618;
    --font: Inter, ui-sans-serif, -apple-system, BlinkMacSystemFont, \"SF Pro Display\",
      \"SF Pro Text\", \"PingFang TC\", \"PingFang SC\", \"Microsoft JhengHei\", sans-serif;
    --mono: \"SF Mono\", \"JetBrains Mono\", ui-monospace, Menlo, Consolas, monospace;
  }}
  * {{ box-sizing: border-box; margin: 0; padding: 0; }}
  html, body {{
    width: {CARD_WIDTH}px;
    height: {CARD_HEIGHT}px;
    overflow: hidden;
    background:
      radial-gradient(circle at 18% 0%, #172135 0, transparent 36%),
      radial-gradient(circle at 100% 30%, #10241f 0, transparent 30%),
      var(--page);
    color: var(--text);
    font-family: var(--font);
    letter-spacing: 0;
  }}
  .frame {{ width: {CARD_WIDTH}px; height: {CARD_HEIGHT}px; padding: 28px; }}
  .card {{
    width: 100%;
    height: 100%;
    overflow: hidden;
    background: linear-gradient(180deg, #111722 0%, var(--panel) 32%, #0e1015 100%);
    border: 2px solid var(--line);
    border-radius: 60px;
    box-shadow: inset 0 1px 0 #303744;
  }}
  .hero {{ padding: 82px 78px 62px; }}
  .eyebrow {{
    display: inline-flex;
    align-items: center;
    gap: 18px;
    padding: 15px 28px;
    color: var(--cyan);
    background: #1b252b;
    border: 2px solid #34424a;
    border-radius: 999px;
    font-family: var(--mono);
    font-size: 29px;
    letter-spacing: 7px;
    text-transform: uppercase;
  }}
  .dot {{
    width: 17px;
    height: 17px;
    border-radius: 50%;
    background: var(--cyan);
    box-shadow: 0 0 20px #6ed7cb80;
  }}
  .title-row {{
    display: flex;
    justify-content: space-between;
    align-items: flex-end;
    gap: 52px;
    margin-top: 70px;
  }}
  h1 {{
    color: var(--text);
    font-size: 88px;
    font-weight: 760;
    line-height: 1.04;
    letter-spacing: -1px;
  }}
  h1 span {{
    color: transparent;
    background: linear-gradient(90deg, var(--blue), var(--cyan));
    -webkit-background-clip: text;
    background-clip: text;
  }}
  .right-tag {{
    padding-bottom: 14px;
    color: var(--dim);
    font-family: var(--mono);
    font-size: 33px;
    white-space: nowrap;
  }}
  .summary {{
    max-width: 1170px;
    margin-top: 56px;
    color: var(--muted);
    font-size: 38px;
    font-weight: 420;
    line-height: 1.48;
  }}
  .summary strong, .change strong {{ color: var(--text); font-weight: 680; }}
  .stats {{
    display: grid;
    grid-template-columns: repeat(4, 1fr);
    border-top: 2px solid var(--line);
    border-bottom: 2px solid var(--line);
  }}
  .stat {{
    min-height: 198px;
    padding: 50px 48px 44px;
    border-right: 2px solid var(--line);
  }}
  .stat:last-child {{ border-right: 0; }}
  .stat .label {{
    color: var(--dim);
    font-family: var(--mono);
    font-size: 26px;
    letter-spacing: 6px;
    text-transform: uppercase;
  }}
  .stat .value {{
    margin-top: 30px;
    color: var(--text);
    font-size: 44px;
    font-weight: 760;
    line-height: 1;
  }}
  .stat .hint {{
    margin-top: 17px;
    color: var(--muted);
    font-size: 28px;
    line-height: 1.15;
  }}
  .section {{
    padding: 64px 78px 58px;
    border-bottom: 2px solid var(--line);
  }}
  .section:last-of-type {{ border-bottom: 0; }}
  .section-label {{
    color: var(--dim);
    font-family: var(--mono);
    font-size: 26px;
    letter-spacing: 8px;
    text-transform: uppercase;
  }}
  .changes {{
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 38px 64px;
    margin-top: 44px;
  }}
  .change {{
    display: grid;
    grid-template-columns: 88px 1fr;
    gap: 36px;
    align-items: start;
  }}
  .icon {{
    display: grid;
    place-items: center;
    width: 88px;
    height: 88px;
    border-radius: 20px;
  }}
  .icon::before {{
    content: attr(data-num);
    font-family: var(--mono);
    font-size: 34px;
    font-weight: 700;
    line-height: 1;
  }}
  .icon.blue {{ color: var(--blue); background: var(--blue-bg); }}
  .icon.cyan {{ color: var(--cyan); background: var(--green-bg); }}
  .icon.orange {{ color: var(--orange); background: var(--orange-bg); }}
  .icon.yellow {{ color: var(--yellow); background: var(--yellow-bg); }}
  .change h2 {{
    color: var(--text);
    font-size: 35px;
    font-weight: 720;
    line-height: 1.22;
  }}
  .change p {{
    margin-top: 12px;
    color: var(--muted);
    font-size: 29px;
    font-weight: 420;
    line-height: 1.36;
  }}
</style>
</head>
<body>
  <main class=\"frame\">
    <article class=\"card\">
      <header class=\"hero\">
        <div class=\"eyebrow\"><span class=\"dot\"></span>Release Notes</div>
        <div class=\"title-row\">
          <h1>Localclash <span>更新日志</span></h1>
          <div class=\"right-tag\">({html.escape(released_channels)})</div>
        </div>
        <p class=\"summary\">{emph(data.summary)}</p>
      </header>

      <section class=\"stats\">
        <div class=\"stat\">
          <div class=\"label\">Core</div>
          <div class=\"value\">{html.escape(data.core_latest)}</div>
          <div class=\"hint\">latest</div>
        </div>
        <div class=\"stat\">
          <div class=\"label\">Core Range</div>
          <div class=\"value\">{html.escape(data.core_range)}</div>
          <div class=\"hint\">{html.escape(data.core_range_hint)}</div>
        </div>
        <div class=\"stat\">
          <div class=\"label\">LuCI</div>
          <div class=\"value\">{html.escape(data.luci_latest)}</div>
          <div class=\"hint\">latest</div>
        </div>
        <div class=\"stat\">
          <div class=\"label\">LuCI Range</div>
          <div class=\"value\">{html.escape(data.luci_range)}</div>
          <div class=\"hint\">{html.escape(data.luci_range_hint)}</div>
        </div>
      </section>

{core_section}
{luci_section}
    </article>
  </main>
</body>
</html>
"""


def render_png(html_path: Path, output: Path, cdp_url: str) -> None:
    try:
        from playwright.sync_api import sync_playwright
    except ImportError as exc:
        raise ScriptError("Python Playwright is required to render the X.com PNG.") from exc

    try:
        with sync_playwright() as p:
            browser = p.chromium.connect_over_cdp(cdp_url)
            contexts_before = tuple(browser.contexts)
            if not contexts_before:
                raise ScriptError(
                    "Arc CDP has no existing browser context; refusing to create an independent Arc window."
                )

            # Arc maps browser.new_context() to an independent window which can
            # become impossible to close. Reuse its existing persistent context
            # and create only a temporary tab in that context.
            context = contexts_before[0]
            pages_before = len(context.pages)
            page = context.new_page()
            try:
                page.set_viewport_size({"width": CARD_WIDTH, "height": CARD_HEIGHT})
                page.goto(html_path.resolve().as_uri(), wait_until="networkidle")
                page.evaluate("() => document.fonts.ready")
                page.evaluate(
                    "() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)))"
                )
                page.screenshot(path=str(output), full_page=False)
            finally:
                page.close()

            pages_after = len(context.pages)
            if pages_after != pages_before:
                raise ScriptError(
                    "Arc CDP temporary tab cleanup failed: "
                    f"pages before={pages_before}, after={pages_after}."
                )
            contexts_after = tuple(browser.contexts)
            if contexts_after != contexts_before:
                raise ScriptError(
                    "Arc CDP browser contexts changed during render; refusing an independent-window result."
                )
            print(
                "Arc CDP render reused existing context: "
                f"contexts={len(contexts_after)}, pages before={pages_before}, after={pages_after}."
            )
    except Exception as exc:
        raise ScriptError(f"Failed to render X.com PNG via Arc CDP at {cdp_url}: {exc}") from exc


def parse_args() -> argparse.Namespace:
    repo_root = Path(__file__).resolve().parents[1]
    parser = argparse.ArgumentParser(description="Generate a localClash X.com release changelog card.")
    parser.add_argument("--changelog", type=Path, default=repo_root / "docs/changelog.md")
    parser.add_argument("--html", type=Path, default=repo_root / "telegram/out" / DEFAULT_HTML_NAME)
    parser.add_argument("--png", type=Path, default=repo_root / "telegram/out" / DEFAULT_PNG_NAME)
    parser.add_argument("--cdp-url", default=DEFAULT_CDP_URL)
    parser.add_argument("--html-only", action="store_true", help="Generate HTML without rendering the PNG.")
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    changelog = read_text(args.changelog)
    data = build_card_data(changelog)
    args.html.parent.mkdir(parents=True, exist_ok=True)
    args.png.parent.mkdir(parents=True, exist_ok=True)
    args.html.write_text(render_html(data), encoding="utf-8")
    print(f"Generated X.com release card HTML: {args.html}")

    if not args.html_only:
        render_png(args.html, args.png, args.cdp_url)
        print(f"Generated X.com release card PNG: {args.png}")
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except ScriptError as exc:
        print(f"Error: {exc}", file=sys.stderr)
        raise SystemExit(1)
