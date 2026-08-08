#!/usr/bin/env python3

from __future__ import annotations

import ast
import importlib.util
import inspect
import sys
import textwrap
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


if __name__ == "__main__":
    unittest.main()
