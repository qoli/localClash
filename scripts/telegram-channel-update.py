#!/usr/bin/env python3
"""Generate and optionally post a localClash Telegram channel update.

The script reads docs/changelog.md, extracts release blocks newer than
telegram/broadcast-state.json, writes the generated update body to
telegram/changelog.md, then combines telegram/top.md + telegram/changelog.md
for previewing or posting to Telegram.
"""

from __future__ import annotations

import argparse
import json
import mimetypes
import os
import re
import sys
import urllib.error
import urllib.parse
import urllib.request
import uuid
from pathlib import Path

CAPTION_LIMIT = 1024
MESSAGE_LIMIT = 4096
DEFAULT_CHAT_ID = "@RonnieAppsChannel"
DEFAULT_SYNCNEXT_TOKEN_FILE = Path("/Volumes/Data/Github/SyncnextProjects/Syncnext/telegram/.token")
DEFAULT_IMAGE_RELATIVE_PATH = Path("telegram/out/localclash-x-release-card.png")
DEFAULT_STATE_RELATIVE_PATH = Path("telegram/broadcast-state.json")


class ScriptError(RuntimeError):
    pass


def read_text(path: Path) -> str:
    return path.read_text(encoding="utf-8")


def write_json(path: Path, data: dict) -> None:
    path.write_text(json.dumps(data, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")


def read_token_from_file(path: Path | None) -> str | None:
    if path is None or not path.exists():
        return None
    token = read_text(path).strip()
    return token or None


def latest_dated_section(markdown: str) -> tuple[str, str]:
    matches = list(re.finditer(r"^## (\d{4}-\d{2}-\d{2})\s*$", markdown, re.MULTILINE))
    if not matches:
        raise ScriptError("No dated changelog section found.")
    match = matches[0]
    start = match.end()
    next_match = re.search(r"^## \S+", markdown[start:], re.MULTILINE)
    end = start + next_match.start() if next_match else len(markdown)
    return match.group(1), markdown[start:end].strip()


def dated_sections(markdown: str) -> list[tuple[str, str]]:
    matches = list(re.finditer(r"^## (\d{4}-\d{2}-\d{2})\s*$", markdown, re.MULTILINE))
    if not matches:
        raise ScriptError("No dated changelog section found.")
    sections: list[tuple[str, str]] = []
    for idx, match in enumerate(matches):
        start = match.end()
        end = matches[idx + 1].start() if idx + 1 < len(matches) else len(markdown)
        sections.append((match.group(1), markdown[start:end].strip()))
    return sections


def strip_markdown_link(text: str) -> str:
    return re.sub(r"\[([^\]]+)\]\(([^)]+)\)", r"\1: \2", text)


def telegram_escape_legacy(text: str) -> str:
    # Legacy Telegram Markdown is used to match the Syncnext sender. Avoid
    # accidental formatting in prose while preserving explicit code backticks.
    parts = text.split("`")
    for idx in range(0, len(parts), 2):
        parts[idx] = parts[idx].replace("*", "").replace("_", "")
    return "`".join(parts)


def normalize_bullet(line: str) -> str:
    line = line.strip()
    if line.startswith("- "):
        line = line[2:].strip()
    return "- " + telegram_escape_legacy(strip_markdown_link(line))


def extract_release_url(block: str) -> str | None:
    match = re.search(r"Release:\s*\n\s*\[[^\]]+\]\(([^)]+)\)", block)
    if match:
        return match.group(1)
    return None


def channel_from_title(title: str) -> str | None:
    lowered = title.lower()
    if "localclash core" in lowered:
        return "core"
    if "localclash-luci" in lowered:
        return "luci"
    return None


def tags_from_title(title: str, channel: str) -> list[str]:
    if channel == "core":
        return re.findall(r"v\d+\.\d+\.\d+", title)
    if channel == "luci":
        return re.findall(r"v\d+\.\d+\.\d+-\d+", title)
    return []


def version_key(tag: str) -> tuple[int, ...]:
    parts = re.findall(r"\d+", tag)
    if not parts:
        raise ScriptError(f"Cannot parse release version from {tag!r}.")
    return tuple(int(part) for part in parts)


def latest_tag(tags: list[str]) -> str:
    if not tags:
        raise ScriptError("Cannot determine latest release tag from changelog block title.")
    return max(tags, key=version_key)


def load_broadcast_state(path: Path) -> dict:
    if not path.exists():
        raise ScriptError(f"Missing Telegram broadcast state: {path}")
    try:
        data = json.loads(read_text(path))
    except json.JSONDecodeError as exc:
        raise ScriptError(f"Invalid Telegram broadcast state JSON: {path}") from exc
    if data.get("schema_version") != 1:
        raise ScriptError(f"Unsupported Telegram broadcast state schema_version in {path}")
    last = data.get("telegram", {}).get("last_announced")
    if not isinstance(last, dict):
        raise ScriptError(f"Invalid Telegram broadcast state: telegram.last_announced is required in {path}")
    for channel in ("core", "luci"):
        tag = last.get(channel)
        if tag is not None and not isinstance(tag, str):
            raise ScriptError(f"Invalid Telegram broadcast state: {channel} tag must be a string")
    return data


def should_announce(tags: list[str], last_announced: str | None) -> bool:
    if not tags:
        return False
    if not last_announced:
        return True
    return version_key(latest_tag(tags)) > version_key(last_announced)


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
            current = line.strip()
            continue
        if current:
            current = join_wrapped_line(current, line)
    if current:
        changes.append(current.strip())
    return changes


def split_release_blocks(section: str) -> list[tuple[str, str]]:
    headings = list(re.finditer(r"^### (.+?)\s*$", section, re.MULTILINE))
    blocks: list[tuple[str, str]] = []
    for idx, heading in enumerate(headings):
        start = heading.end()
        end = headings[idx + 1].start() if idx + 1 < len(headings) else len(section)
        blocks.append((heading.group(1).strip(), section[start:end].strip()))
    return blocks


def merge_text(top: str, changelog: str, date: str) -> str:
    top_clean = top.replace("{date}", date).rstrip("\n")
    change_clean = changelog.lstrip("\n")
    if top_clean and change_clean:
        return f"{top_clean}\n\n{change_clean}"
    return f"{top_clean}{change_clean}"


def build_telegram_changelog(changelog: str, state: dict) -> tuple[str, str, dict]:
    last_announced = state["telegram"]["last_announced"]

    lines: list[str] = []
    extracted_blocks = 0
    latest_announced = dict(last_announced)
    source_date = ""
    for date, section in dated_sections(changelog):
        blocks = split_release_blocks(section)
        if not blocks:
            continue
        for title, block in blocks:
            channel = channel_from_title(title)
            if channel is None:
                continue
            tags = tags_from_title(title, channel)
            if not should_announce(tags, last_announced.get(channel)):
                continue
            changes = extract_changes(block)
            release_url = extract_release_url(block)
            if not changes:
                continue

            if not source_date:
                source_date = date
            extracted_blocks += 1
            candidate = latest_tag(tags)
            current = latest_announced.get(channel)
            if not current or version_key(candidate) > version_key(current):
                latest_announced[channel] = candidate
            lines.append("")
            lines.append(f"*{telegram_escape_legacy(title)}*")
            for change in changes:
                lines.append(normalize_bullet(change))
            if release_url:
                lines.append(f"Release: {release_url}")

    message = "\n".join(lines).strip() + "\n"
    if extracted_blocks == 0:
        raise ScriptError("No unannounced Telegram release blocks found.")
    updated_state = dict(state)
    updated_state["telegram"] = dict(state["telegram"])
    updated_state["telegram"]["last_announced"] = latest_announced
    return source_date, message, updated_state


def build_multipart(fields: dict[str, str], files: dict[str, tuple[str, bytes, str]]) -> tuple[bytes, str]:
    boundary = f"----tg-boundary-{uuid.uuid4().hex}"
    lines: list[bytes] = []

    for name, value in fields.items():
        lines.append(f"--{boundary}".encode())
        lines.append(f'Content-Disposition: form-data; name="{name}"'.encode())
        lines.append(b"")
        lines.append(value.encode())

    for name, (filename, data, mime) in files.items():
        lines.append(f"--{boundary}".encode())
        lines.append(f'Content-Disposition: form-data; name="{name}"; filename="{filename}"'.encode())
        lines.append(f"Content-Type: {mime}".encode())
        lines.append(b"")
        lines.append(data)

    lines.append(f"--{boundary}--".encode())
    lines.append(b"")
    return b"\r\n".join(lines), f"multipart/form-data; boundary={boundary}"


def http_post(url: str, fields: dict[str, str], files: dict[str, tuple[str, bytes, str]] | None = None) -> dict:
    if files:
        body, content_type = build_multipart(fields, files)
        headers = {"Content-Type": content_type}
    else:
        body = urllib.parse.urlencode(fields).encode()
        headers = {"Content-Type": "application/x-www-form-urlencoded"}

    request = urllib.request.Request(url, data=body, headers=headers, method="POST")
    try:
        with urllib.request.urlopen(request) as response:
            data = response.read().decode("utf-8")
    except urllib.error.HTTPError as exc:
        data = exc.read().decode("utf-8", errors="replace")
        raise ScriptError(f"HTTP {exc.code} {exc.reason}: {data}") from exc
    except urllib.error.URLError as exc:
        raise ScriptError(f"Request failed: {exc.reason}") from exc

    try:
        return json.loads(data)
    except json.JSONDecodeError as exc:
        raise ScriptError(f"Invalid JSON response: {data}") from exc


def chunk_text(text: str, limit: int) -> list[str]:
    chunks: list[str] = []
    remaining = text
    while len(remaining) > limit:
        split_at = remaining.rfind("\n", 0, limit)
        if split_at == -1:
            split_at = remaining.rfind(" ", 0, limit)
        if split_at == -1:
            split_at = limit
        chunks.append(remaining[:split_at].rstrip("\n"))
        remaining = remaining[split_at:].lstrip("\n")
    if remaining:
        chunks.append(remaining)
    return chunks


def send_message(api_base: str, chat_id: str, text: str, parse_mode: str | None) -> dict:
    fields = {"chat_id": chat_id, "text": text}
    if parse_mode:
        fields["parse_mode"] = parse_mode
    return http_post(f"{api_base}/sendMessage", fields)


def send_photo(api_base: str, chat_id: str, image_path: Path, caption: str | None, parse_mode: str | None) -> dict:
    mime, _ = mimetypes.guess_type(str(image_path))
    if not mime:
        mime = "application/octet-stream"
    fields = {"chat_id": chat_id}
    if caption:
        fields["caption"] = caption
    if parse_mode:
        fields["parse_mode"] = parse_mode
    files = {"photo": (image_path.name, image_path.read_bytes(), mime)}
    return http_post(f"{api_base}/sendPhoto", fields, files)


def validate_delivery_shape(message: str, image: Path | None) -> None:
    if image and len(message) > CAPTION_LIMIT:
        raise ScriptError(
            f"Telegram image caption is too long: {len(message)} > {CAPTION_LIMIT}; shorten the announcement."
        )


def post_to_telegram(token: str, chat_id: str, message: str, image: Path | None, parse_mode: str | None) -> None:
    api_base = f"https://api.telegram.org/bot{token}"
    if image:
        validate_delivery_shape(message, image)
        response = send_photo(api_base, chat_id, image, message, parse_mode)
        if not response.get("ok"):
            raise ScriptError(f"sendPhoto failed: {response}")
        print("Posted Telegram photo with caption.")
        return

    for chunk in chunk_text(message, MESSAGE_LIMIT):
        response = send_message(api_base, chat_id, chunk, parse_mode)
        if not response.get("ok"):
            raise ScriptError(f"sendMessage failed: {response}")
    print("Posted Telegram message.")


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Generate or post a localClash Telegram channel update.")
    repo_root = Path(__file__).resolve().parents[1]
    parser.add_argument("--changelog", type=Path, default=repo_root / "docs/changelog.md")
    parser.add_argument("--top", type=Path, default=repo_root / "telegram/top.md")
    parser.add_argument("--output", type=Path, default=repo_root / "telegram/changelog.md")
    parser.add_argument("--state", type=Path, default=repo_root / DEFAULT_STATE_RELATIVE_PATH)
    parser.add_argument("--chat-id", default=DEFAULT_CHAT_ID)
    parser.add_argument("--token", default=None, help="Telegram bot token. Prefer env or token files.")
    parser.add_argument("--token-file", type=Path, default=repo_root / "telegram/.token")
    parser.add_argument(
        "--syncnext-token-file",
        type=Path,
        default=DEFAULT_SYNCNEXT_TOKEN_FILE,
        help="Fallback token file used by the existing Syncnext Telegram workflow.",
    )
    parser.add_argument(
        "--image",
        type=Path,
        default=repo_root / DEFAULT_IMAGE_RELATIVE_PATH,
        help="Image to send before/as the update. Defaults to the generated X changelog card.",
    )
    parser.add_argument("--no-image", action="store_true", help="Send text only, without the default image.")
    parser.add_argument("--parse-mode", default="Markdown", help="Telegram parse mode. Use empty string for plain text.")
    parser.add_argument("--dry-run", action="store_true", help="Generate and print the message without posting.")
    parser.add_argument("--no-write", action="store_true", help="Print/generate without writing --output.")
    parser.add_argument("--no-state-update", action="store_true", help="Do not update broadcast state after posting.")
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    state = load_broadcast_state(args.state)
    date, changelog_body, updated_state = build_telegram_changelog(read_text(args.changelog), state)

    if not args.no_write:
        args.output.parent.mkdir(parents=True, exist_ok=True)
        args.output.write_text(changelog_body, encoding="utf-8")

    message = merge_text(read_text(args.top), changelog_body, date) + "\n"
    image = None if args.no_image else args.image
    validate_delivery_shape(message, image)

    if args.dry_run:
        print(message, end="")
        if not args.no_write:
            print(f"\nGenerated Telegram update: {args.output}", file=sys.stderr)
        print(f"Source changelog date: {date}", file=sys.stderr)
        return 0

    token = (
        args.token
        or os.environ.get("TELEGRAM_BOT_TOKEN")
        or read_token_from_file(args.token_file)
        or read_token_from_file(args.syncnext_token_file)
    )
    if not token:
        raise ScriptError("Missing Telegram bot token. Set TELEGRAM_BOT_TOKEN or create telegram/.token.")

    if image and not image.exists():
        raise ScriptError(f"Image file not found: {image}")
    parse_mode = args.parse_mode or None
    post_to_telegram(token, args.chat_id, message, image, parse_mode)
    if not args.no_state_update:
        write_json(args.state, updated_state)
        print(f"Updated Telegram broadcast state: {args.state}")
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except ScriptError as exc:
        print(f"Error: {exc}", file=sys.stderr)
        raise SystemExit(1)
