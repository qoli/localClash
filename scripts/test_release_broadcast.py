#!/usr/bin/env python3

from __future__ import annotations

import ast
import importlib.util
import inspect
import json
import sys
import textwrap
import tempfile
import unittest
from pathlib import Path


REPO_ROOT = Path(__file__).resolve().parents[1]


def load_script(name: str, filename: str):
    path = REPO_ROOT / "scripts" / filename
    spec = importlib.util.spec_from_file_location(name, path)
    if spec is None or spec.loader is None:
        raise RuntimeError(f"cannot load {path}")
    module = importlib.util.module_from_spec(spec)
    sys.modules[name] = module
    spec.loader.exec_module(module)
    return module


class ReleaseCardRendererSafetyTests(unittest.TestCase):
    def test_card_can_select_one_release_channel(self) -> None:
        module = load_script("x_release_card_channel_test", "x-release-card.py")
        changelog = """# 更新日誌

| 渠道 | 最新版本 | 發佈時間 |
| --- | --- | --- |
| localClash Core | [v0.1.49](https://example.com/core) | today |
| localclash-luci | [v0.1.0-43](https://example.com/luci) | today |

## 2026-08-09

### localClash Core v0.1.49

Changes:

- Core change，core body。

### localclash-luci v0.1.0-43

Changes:

- LuCI change，luci body。
"""
        core = module.build_card_data(changelog, channel_filter="core")
        self.assertEqual(len(core.core_items), 1)
        self.assertEqual(core.luci_items, [])
        self.assertIn("Core change", core.summary)
        self.assertNotIn("LuCI change", core.summary)

    def test_arc_renderer_reuses_existing_context(self) -> None:
        module = load_script("x_release_card_test", "x-release-card.py")
        source = textwrap.dedent(inspect.getsource(module.render_png))
        tree = ast.parse(source)
        forbidden: list[str] = []
        for node in ast.walk(tree):
            if not isinstance(node, ast.Call) or not isinstance(node.func, ast.Attribute):
                continue
            receiver = node.func.value
            if not isinstance(receiver, ast.Name) or receiver.id != "browser":
                continue
            if node.func.attr in {"new_context", "close"}:
                forbidden.append(f"browser.{node.func.attr}()")

        self.assertEqual(forbidden, [])
        reuses_existing_context = any(
            isinstance(node, ast.Assign)
            and any(isinstance(target, ast.Name) and target.id == "context" for target in node.targets)
            and isinstance(node.value, ast.Subscript)
            and isinstance(node.value.value, ast.Name)
            and node.value.value.id == "contexts_before"
            for node in ast.walk(tree)
        )
        self.assertTrue(reuses_existing_context)
        closes_page_in_finally = any(
            isinstance(statement, ast.Expr)
            and isinstance(statement.value, ast.Call)
            and isinstance(statement.value.func, ast.Attribute)
            and isinstance(statement.value.func.value, ast.Name)
            and statement.value.func.value.id == "page"
            and statement.value.func.attr == "close"
            for node in ast.walk(tree)
            if isinstance(node, ast.Try)
            for statement in node.finalbody
        )
        self.assertTrue(closes_page_in_finally)


class XPostPublisherTests(unittest.TestCase):
    def setUp(self) -> None:
        self.module = load_script(f"x_post_test_{id(self)}", "x-post.py")
        self.temporary = tempfile.TemporaryDirectory()
        self.root = Path(self.temporary.name)
        self.image = self.root / "card.png"
        self.image.write_bytes(b"\x89PNG\r\n\x1a\nfixture")
        self.state = self.root / "state.json"
        self.state.write_text('{"schema_version": 1, "posts": []}\n', encoding="utf-8")

    def tearDown(self) -> None:
        self.temporary.cleanup()

    def request(self, text: str = "release text"):
        return self.module.PublishRequest(account="@llqoli", text=text, image=self.image)

    def test_publish_once_records_verified_receipt(self) -> None:
        module = self.module
        prepared = module.prepare_post(self.request())

        class FakePublisher:
            calls = 0

            def publish(self, post):
                self.calls += 1
                return module.PublishReceipt(
                    account=post.request.account,
                    status_url="https://x.com/llqoli/status/123",
                    text_sha256=post.text_sha256,
                    image_sha256=post.image_sha256,
                    fingerprint=post.fingerprint,
                    verified=True,
                )

        publisher = FakePublisher()
        receipt = module.publish_once(prepared.request, self.state, publisher)
        self.assertEqual(publisher.calls, 1)
        self.assertEqual(receipt.status_url, "https://x.com/llqoli/status/123")
        saved = json.loads(self.state.read_text(encoding="utf-8"))
        self.assertEqual(saved["posts"], [module.asdict(receipt)])

    def test_duplicate_fingerprint_fails_before_publisher(self) -> None:
        module = self.module
        prepared = module.prepare_post(self.request())
        self.state.write_text(
            json.dumps(
                {
                    "schema_version": 1,
                    "posts": [
                        {
                            "account": "@llqoli",
                            "status_url": "https://x.com/llqoli/status/123",
                            "text_sha256": prepared.text_sha256,
                            "image_sha256": prepared.image_sha256,
                            "fingerprint": prepared.fingerprint,
                            "verified": True,
                        }
                    ],
                }
            ),
            encoding="utf-8",
        )

        class UnexpectedPublisher:
            def publish(self, post):
                raise AssertionError("publisher must not be called for a duplicate")

        with self.assertRaisesRegex(module.ScriptError, "already published"):
            module.publish_once(prepared.request, self.state, UnexpectedPublisher())

    def test_missing_or_malformed_state_fails_explicitly(self) -> None:
        missing = self.root / "missing.json"
        with self.assertRaisesRegex(self.module.ScriptError, "Missing X broadcast state"):
            self.module.load_state(missing)
        self.state.write_text("{}", encoding="utf-8")
        with self.assertRaisesRegex(self.module.ScriptError, "schema_version"):
            self.module.load_state(self.state)

    def test_empty_text_and_missing_image_fail_explicitly(self) -> None:
        with self.assertRaisesRegex(self.module.ScriptError, "must not be empty"):
            self.module.prepare_post(self.request("  "))
        missing_request = self.module.PublishRequest(
            account="@llqoli", text="release text", image=self.root / "missing.png"
        )
        with self.assertRaisesRegex(self.module.ScriptError, "image file not found"):
            self.module.prepare_post(missing_request)
        invalid_image = self.root / "invalid.png"
        invalid_image.write_bytes(b"not-an-image")
        with self.assertRaisesRegex(self.module.ScriptError, "must be a PNG"):
            self.module.prepare_post(
                self.module.PublishRequest(account="@llqoli", text="release text", image=invalid_image)
            )

    def test_composer_requires_exact_inner_text_image_and_enabled_button(self) -> None:
        module = self.module
        module.validate_composer("expected", "expected", 1, True)
        with self.assertRaisesRegex(module.ScriptError, "text mismatch"):
            module.validate_composer("expected", "", 1, True)
        with self.assertRaisesRegex(module.ScriptError, "exactly one image"):
            module.validate_composer("expected", "expected", 0, True)
        with self.assertRaisesRegex(module.ScriptError, "button is disabled"):
            module.validate_composer("expected", "expected", 1, False)

    def test_failed_publish_does_not_mutate_state_or_retry(self) -> None:
        module = self.module
        original = self.state.read_text(encoding="utf-8")

        class FailingPublisher:
            calls = 0

            def publish(self, post):
                self.calls += 1
                raise module.ScriptError("unknown submission state; do not retry")

        publisher = FailingPublisher()
        with self.assertRaisesRegex(module.ScriptError, "do not retry"):
            module.publish_once(self.request(), self.state, publisher)
        self.assertEqual(publisher.calls, 1)
        self.assertEqual(self.state.read_text(encoding="utf-8"), original)

    def test_create_tweet_response_requires_numeric_status_id(self) -> None:
        valid = {"data": {"create_tweet": {"tweet_results": {"result": {"rest_id": "456"}}}}}
        self.assertEqual(self.module.extract_status_id(valid), "456")
        with self.assertRaisesRegex(self.module.ScriptError, "valid numeric rest_id"):
            self.module.extract_status_id(
                {"data": {"create_tweet": {"tweet_results": {"result": {"rest_id": "bad"}}}}}
            )

    def test_arc_adapter_contains_one_post_click_and_no_retry_loop(self) -> None:
        source = inspect.getsource(self.module.ArcCDPPublisher._publish_in_page)
        tree = ast.parse(textwrap.dedent(source))
        post_clicks = [
            node
            for node in ast.walk(tree)
            if isinstance(node, ast.Call)
            and isinstance(node.func, ast.Attribute)
            and isinstance(node.func.value, ast.Name)
            and node.func.value.id == "post_button"
            and node.func.attr == "click"
        ]
        retry_loops = [node for node in ast.walk(tree) if isinstance(node, (ast.For, ast.While))]
        self.assertEqual(len(post_clicks), 1)
        self.assertEqual(retry_loops, [])
        self.assertIn("editor.fill(post.request.text)", source)
        self.assertNotIn("editor.press_sequentially", source)

        self.assertIn(
            "page.locator('div[role=\"dialog\"][aria-modal=\"true\"]')",
            source,
        )
        for scoped_locator in (
            "composer.locator('div[data-testid=\"tweetTextarea_0\"]')",
            "composer.locator('input[data-testid=\"fileInput\"]')",
            "composer.locator('div[data-testid=\"attachments\"] img')",
            "composer.locator('button[data-testid=\"tweetButton\"]')",
        ):
            self.assertIn(scoped_locator, source)
        self.assertNotIn(
            "page.locator('div[data-testid=\"tweetTextarea_0\"]')",
            source,
        )

        adapter_source = inspect.getsource(self.module.ArcCDPPublisher.publish)
        adapter_tree = ast.parse(textwrap.dedent(adapter_source))
        forbidden_browser_calls = [
            node.func.attr
            for node in ast.walk(adapter_tree)
            if isinstance(node, ast.Call)
            and isinstance(node.func, ast.Attribute)
            and isinstance(node.func.value, ast.Name)
            and node.func.value.id == "browser"
            and node.func.attr in {"new_context", "close"}
        ]
        self.assertEqual(forbidden_browser_calls, [])
        closes_temporary_page_in_finally = any(
            isinstance(statement, ast.Expr)
            and isinstance(statement.value, ast.Call)
            and isinstance(statement.value.func, ast.Attribute)
            and isinstance(statement.value.func.value, ast.Name)
            and statement.value.func.value.id == "page"
            and statement.value.func.attr == "close"
            for node in ast.walk(adapter_tree)
            if isinstance(node, ast.Try)
            for statement in node.finalbody
        )
        self.assertTrue(closes_temporary_page_in_finally)


if __name__ == "__main__":
    unittest.main()
