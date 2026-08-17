#!/usr/bin/env python3
"""Publish one verified X.com post through the existing Arc CDP session."""

from __future__ import annotations

import argparse
import fcntl
import hashlib
import json
import os
import re
import sys
import tempfile
from dataclasses import asdict, dataclass
from pathlib import Path
from typing import Any, Protocol


DEFAULT_CDP_URL = "http://localhost:9222"
DEFAULT_COMPOSE_URL = "https://x.com/compose/post"
DEFAULT_STATE_RELATIVE_PATH = Path("x/broadcast-state.json")
ACCOUNT_PATTERN = re.compile(r"^@[A-Za-z0-9_]{1,15}$")
STATUS_URL_PATTERN = re.compile(r"^https://x\.com/([A-Za-z0-9_]{1,15})/status/(\d+)$")
SHA256_PATTERN = re.compile(r"^[0-9a-f]{64}$")


class ScriptError(RuntimeError):
    pass


@dataclass(frozen=True)
class PublishRequest:
    account: str
    text: str
    image: Path


@dataclass(frozen=True)
class PreparedPost:
    request: PublishRequest
    text_sha256: str
    image_sha256: str
    fingerprint: str


@dataclass(frozen=True)
class PublishReceipt:
    account: str
    status_url: str
    text_sha256: str
    image_sha256: str
    fingerprint: str
    verified: bool


class Publisher(Protocol):
    def publish(self, post: PreparedPost) -> PublishReceipt:
        """Submit exactly once and return a verified receipt."""


def sha256_bytes(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()


def detect_image_type(data: bytes) -> str:
    if data.startswith(b"\x89PNG\r\n\x1a\n"):
        return "png"
    if data.startswith(b"\xff\xd8\xff"):
        return "jpeg"
    if data.startswith((b"GIF87a", b"GIF89a")):
        return "gif"
    if len(data) >= 12 and data.startswith(b"RIFF") and data[8:12] == b"WEBP":
        return "webp"
    raise ScriptError("X post image must be a PNG, JPEG, GIF, or WebP file.")


def image_mime_type(image_type: str) -> str:
    return {
        "png": "image/png",
        "jpeg": "image/jpeg",
        "gif": "image/gif",
        "webp": "image/webp",
    }[image_type]


def prepare_post(request: PublishRequest) -> PreparedPost:
    if not ACCOUNT_PATTERN.fullmatch(request.account):
        raise ScriptError(f"Invalid X account handle: {request.account!r}")
    if not request.text or not request.text.strip():
        raise ScriptError("X post text must not be empty.")
    if "\x00" in request.text:
        raise ScriptError("X post text must not contain NUL bytes.")
    if not request.image.is_file():
        raise ScriptError(f"X post image file not found: {request.image}")
    image_bytes = request.image.read_bytes()
    if not image_bytes:
        raise ScriptError(f"X post image file is empty: {request.image}")
    detect_image_type(image_bytes)

    text_sha256 = sha256_bytes(request.text.encode("utf-8"))
    image_sha256 = sha256_bytes(image_bytes)
    fingerprint_material = "\0".join((request.account.lower(), text_sha256, image_sha256))
    return PreparedPost(
        request=request,
        text_sha256=text_sha256,
        image_sha256=image_sha256,
        fingerprint=sha256_bytes(fingerprint_material.encode("ascii")),
    )


def load_state(path: Path) -> dict[str, Any]:
    if not path.is_file():
        raise ScriptError(f"Missing X broadcast state: {path}")
    try:
        state = json.loads(path.read_text(encoding="utf-8"))
    except json.JSONDecodeError as exc:
        raise ScriptError(f"Malformed X broadcast state JSON: {path}: {exc}") from exc
    if state.get("schema_version") != 1:
        raise ScriptError(f"Unsupported X broadcast state schema_version in {path}")
    posts = state.get("posts")
    if not isinstance(posts, list):
        raise ScriptError(f"Malformed X broadcast state: posts must be an array in {path}")
    for index, item in enumerate(posts):
        if not isinstance(item, dict):
            raise ScriptError(f"Malformed X broadcast state: posts[{index}] must be an object")
        for field in ("account", "status_url", "text_sha256", "image_sha256", "fingerprint"):
            if not isinstance(item.get(field), str) or not item[field]:
                raise ScriptError(f"Malformed X broadcast state: posts[{index}].{field} is required")
        if not ACCOUNT_PATTERN.fullmatch(item["account"]):
            raise ScriptError(f"Malformed X broadcast state: posts[{index}].account is invalid")
        status_match = STATUS_URL_PATTERN.fullmatch(item["status_url"])
        if not status_match or status_match.group(1).lower() != item["account"][1:].lower():
            raise ScriptError(f"Malformed X broadcast state: posts[{index}].status_url is invalid")
        for field in ("text_sha256", "image_sha256", "fingerprint"):
            if not SHA256_PATTERN.fullmatch(item[field]):
                raise ScriptError(f"Malformed X broadcast state: posts[{index}].{field} is invalid")
        if item.get("verified") is not True:
            raise ScriptError(f"Malformed X broadcast state: posts[{index}].verified must be true")
    return state


def assert_not_published(post: PreparedPost, state: dict[str, Any]) -> None:
    for item in state["posts"]:
        if item["fingerprint"] == post.fingerprint:
            raise ScriptError(
                "X post content was already published: "
                f"{item['status_url']} (fingerprint={post.fingerprint})"
            )


def write_state(path: Path, state: dict[str, Any]) -> None:
    payload = json.dumps(state, ensure_ascii=False, indent=2) + "\n"
    path.parent.mkdir(parents=True, exist_ok=True)
    descriptor, temporary_name = tempfile.mkstemp(prefix=f".{path.name}.", dir=path.parent)
    temporary_path = Path(temporary_name)
    try:
        with os.fdopen(descriptor, "w", encoding="utf-8") as handle:
            handle.write(payload)
            handle.flush()
            os.fsync(handle.fileno())
        os.replace(temporary_path, path)
    except Exception:
        temporary_path.unlink(missing_ok=True)
        raise


def record_receipt(path: Path, state: dict[str, Any], receipt: PublishReceipt) -> None:
    if not receipt.verified:
        raise ScriptError("Refusing to record an unverified X post receipt.")
    updated = {"schema_version": 1, "posts": [*state["posts"], asdict(receipt)]}
    try:
        write_state(path, updated)
    except Exception as exc:
        raise ScriptError(
            "X post was published and verified, but its receipt could not be recorded; "
            f"do not retry. status_url={receipt.status_url}: {exc}"
        ) from exc


def publish_once(
    request: PublishRequest,
    state_path: Path,
    publisher: Publisher,
) -> PublishReceipt:
    post = prepare_post(request)
    load_state(state_path)
    lock_path = state_path.with_name(f".{state_path.name}.lock")
    with lock_path.open("a+", encoding="utf-8") as lock:
        fcntl.flock(lock.fileno(), fcntl.LOCK_EX)
        state = load_state(state_path)
        assert_not_published(post, state)
        receipt = publisher.publish(post)
        if receipt.account != post.request.account:
            raise ScriptError(
                "X may have been published but the receipt account is wrong; do not retry. "
                f"expected {post.request.account}, got {receipt.account}"
            )
        if receipt.text_sha256 != post.text_sha256 or receipt.image_sha256 != post.image_sha256:
            raise ScriptError(
                "X may have been published but the receipt content is wrong; do not retry."
            )
        if receipt.fingerprint != post.fingerprint:
            raise ScriptError(
                "X may have been published but the receipt fingerprint is wrong; do not retry."
            )
        match = STATUS_URL_PATTERN.fullmatch(receipt.status_url)
        if not match or match.group(1).lower() != post.request.account[1:].lower():
            raise ScriptError(
                "X may have been published but the receipt status URL is invalid; do not retry. "
                f"status_url={receipt.status_url}"
            )
        record_receipt(state_path, state, receipt)
        return receipt


def validate_composer(
    expected_text: str,
    actual_inner_text: str,
    image_preview_count: int,
    post_button_enabled: bool,
) -> None:
    if actual_inner_text != expected_text:
        raise ScriptError(
            "X composer text mismatch; refusing to publish. "
            f"expected_sha256={sha256_bytes(expected_text.encode('utf-8'))}, "
            f"actual_sha256={sha256_bytes(actual_inner_text.encode('utf-8'))}"
        )
    if image_preview_count != 1:
        raise ScriptError(
            f"X composer must contain exactly one image preview; found {image_preview_count}."
        )
    if not post_button_enabled:
        raise ScriptError("X composer Post button is disabled; refusing to publish.")


def extract_status_id(create_tweet_response: dict[str, Any]) -> str:
    try:
        result = create_tweet_response["data"]["create_tweet"]["tweet_results"]["result"]
    except (KeyError, TypeError) as exc:
        raise ScriptError("CreateTweet response does not contain tweet_results.result.") from exc
    status_id = result.get("rest_id") if isinstance(result, dict) else None
    if not isinstance(status_id, str) or not status_id.isdigit():
        raise ScriptError("CreateTweet response does not contain a valid numeric rest_id.")
    return status_id


class ArcCDPPublisher:
    """X.com adapter that reuses Arc's existing persistent browser context."""

    def __init__(self, cdp_url: str = DEFAULT_CDP_URL) -> None:
        self.cdp_url = cdp_url

    def publish(self, post: PreparedPost) -> PublishReceipt:
        try:
            from playwright.sync_api import TimeoutError as PlaywrightTimeoutError
            from playwright.sync_api import sync_playwright
        except ImportError as exc:
            raise ScriptError("Python Playwright is required to publish an X post.") from exc

        try:
            with sync_playwright() as playwright:
                browser = playwright.chromium.connect_over_cdp(self.cdp_url)
                contexts_before = tuple(browser.contexts)
                if not contexts_before:
                    raise ScriptError(
                        "Arc CDP has no existing browser context; refusing to create an independent window."
                    )
                context = contexts_before[0]
                pages_before = len(context.pages)
                page = context.new_page()
                try:
                    receipt = self._publish_in_page(page, post, PlaywrightTimeoutError)
                finally:
                    page.close()
                if len(context.pages) != pages_before:
                    raise ScriptError("Arc CDP temporary X tab cleanup failed.")
                if tuple(browser.contexts) != contexts_before:
                    raise ScriptError("Arc CDP browser contexts changed while publishing the X post.")
                return receipt
        except ScriptError:
            raise
        except Exception as exc:
            raise ScriptError(f"Failed to publish X post through Arc CDP at {self.cdp_url}: {exc}") from exc

    def _publish_in_page(self, page: Any, post: PreparedPost, timeout_error: type[Exception]) -> PublishReceipt:
        account_name = post.request.account[1:]
        page.goto(DEFAULT_COMPOSE_URL, wait_until="domcontentloaded")

        profile_link = page.locator('a[data-testid="AppTabBar_Profile_Link"]')
        profile_link.wait_for(state="attached")
        profile_href = profile_link.get_attribute("href")
        if profile_href != f"/{account_name}":
            raise ScriptError(
                f"Arc is signed into the wrong X account: expected /{account_name}, got {profile_href!r}"
            )

        editor = page.locator('div[data-testid="tweetTextarea_0"]')
        editor.wait_for(state="visible")
        file_input = page.locator('input[data-testid="fileInput"]').first
        image_bytes = post.request.image.read_bytes()
        if sha256_bytes(image_bytes) != post.image_sha256:
            raise ScriptError("X post image changed after validation; refusing to publish.")
        image_type = detect_image_type(image_bytes)
        file_input.set_input_files(
            {
                "name": post.request.image.name,
                "mimeType": image_mime_type(image_type),
                "buffer": image_bytes,
            }
        )
        image_preview = page.locator('div[data-testid="attachments"] img')
        image_preview.wait_for(state="visible")

        editor.click()
        editor.press_sequentially(post.request.text)
        actual_text = editor.evaluate("element => element.innerText")
        post_button = page.locator('button[data-testid="tweetButton"]')
        post_button.wait_for(state="visible")
        validate_composer(
            expected_text=post.request.text,
            actual_inner_text=actual_text,
            image_preview_count=image_preview.count(),
            post_button_enabled=post_button.is_enabled(),
        )

        try:
            with page.expect_response(lambda response: "CreateTweet" in response.url) as response_info:
                post_button.click()
            response = response_info.value
            if not response.ok:
                raise ScriptError(f"X CreateTweet failed with HTTP {response.status}.")
            try:
                response_body = response.json()
                status_id = extract_status_id(response_body)
            except Exception as exc:
                raise ScriptError(
                    "X submission may have succeeded but its CreateTweet receipt is malformed; do not retry."
                ) from exc
        except timeout_error as exc:
            raise ScriptError(
                "X submission status is unknown because no CreateTweet response was observed; do not retry."
            ) from exc

        status_url = f"https://x.com/{account_name}/status/{status_id}"
        try:
            self._verify_status(page, post, status_url)
        except Exception as exc:
            raise ScriptError(
                "X post was created but final verification failed; do not retry. "
                f"status_url={status_url}: {exc}"
            ) from exc
        return PublishReceipt(
            account=post.request.account,
            status_url=status_url,
            text_sha256=post.text_sha256,
            image_sha256=post.image_sha256,
            fingerprint=post.fingerprint,
            verified=True,
        )

    def _verify_status(self, page: Any, post: PreparedPost, status_url: str) -> None:
        page.goto(status_url, wait_until="domcontentloaded")
        article = page.locator('article[data-testid="tweet"]').first
        article.wait_for(state="visible")
        account_name = post.request.account[1:]
        if article.locator(f'a[href="/{account_name}"]').count() < 1:
            raise ScriptError(f"Published X post does not show the expected account {post.request.account}.")

        text = article.locator('div[data-testid="tweetText"]')
        text.wait_for(state="visible")
        actual_visible_text = text.evaluate("element => element.innerText")
        expected_lines = [line for line in post.request.text.splitlines() if not line.startswith(("http://", "https://"))]
        missing_lines = [line for line in expected_lines if line not in actual_visible_text]
        if missing_lines:
            raise ScriptError(f"Published X post is missing expected text lines: {missing_lines!r}")

        expected_urls = re.findall(r"https?://\S+", post.request.text)
        link_targets = text.locator("a").evaluate_all(
            """elements => elements.flatMap(element => [
                element.href,
                element.getAttribute('data-expanded-url'),
                element.getAttribute('title')
            ]).filter(Boolean)"""
        )
        for expected_url in expected_urls:
            if expected_url not in link_targets:
                raise ScriptError(f"Published X post is missing expected link target: {expected_url}")
        if article.locator('a[href$="/photo/1"] img').count() != 1:
            raise ScriptError("Published X post does not contain exactly one image.")


def read_post_text(path: Path) -> str:
    if not path.is_file():
        raise ScriptError(f"X post text file not found: {path}")
    return path.read_text(encoding="utf-8").rstrip("\r\n")


def parse_args() -> argparse.Namespace:
    repo_root = Path(__file__).resolve().parents[1]
    parser = argparse.ArgumentParser(description="Publish one verified X.com post through Arc CDP.")
    parser.add_argument("--account", required=True, help="Exact signed-in X handle, including @.")
    parser.add_argument("--text-file", type=Path, required=True, help="UTF-8 file containing the exact post text.")
    parser.add_argument("--image", type=Path, required=True, help="Single image to attach to the post.")
    parser.add_argument("--state", type=Path, default=repo_root / DEFAULT_STATE_RELATIVE_PATH)
    parser.add_argument("--cdp-url", default=DEFAULT_CDP_URL)
    parser.add_argument(
        "--publish",
        action="store_true",
        help="Perform the single live submission. Without this flag only local validation runs.",
    )
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    request = PublishRequest(account=args.account, text=read_post_text(args.text_file), image=args.image)
    post = prepare_post(request)
    state = load_state(args.state)
    assert_not_published(post, state)
    if not args.publish:
        print(
            json.dumps(
                {
                    "account": request.account,
                    "text_sha256": post.text_sha256,
                    "image_sha256": post.image_sha256,
                    "fingerprint": post.fingerprint,
                    "publish": False,
                    "validated": True,
                },
                ensure_ascii=False,
                indent=2,
            )
        )
        return 0

    receipt = publish_once(request, args.state, ArcCDPPublisher(args.cdp_url))
    print(json.dumps(asdict(receipt), ensure_ascii=False, indent=2))
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except ScriptError as exc:
        print(f"Error: {exc}", file=sys.stderr)
        raise SystemExit(1)
