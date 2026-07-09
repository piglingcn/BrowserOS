#!/usr/bin/env python3
"""Tests for human-editable build flags."""

import tempfile
import unittest
from pathlib import Path
from types import SimpleNamespace

from .build_flags import BuildFlags, build_flags_for_context, load_build_flags


class BuildFlagsTest(unittest.TestCase):
    def test_missing_config_defaults_to_rust_claw_server(self):
        with tempfile.TemporaryDirectory() as tmp:
            flags = load_build_flags(Path(tmp))

        self.assertTrue(flags.use_claw_server_rust)

    def test_missing_key_defaults_to_rust_claw_server(self):
        with tempfile.TemporaryDirectory() as tmp:
            config = Path(tmp) / "bos_build" / "config" / "build_flags.yaml"
            config.parent.mkdir(parents=True)
            config.write_text("{}\n")

            flags = load_build_flags(Path(tmp))

        self.assertTrue(flags.use_claw_server_rust)

    def test_explicit_false_disables_rust_claw_server(self):
        with tempfile.TemporaryDirectory() as tmp:
            config = Path(tmp) / "bos_build" / "config" / "build_flags.yaml"
            config.parent.mkdir(parents=True)
            config.write_text("use_claw_server_rust: false\n")

            flags = load_build_flags(Path(tmp))

        self.assertFalse(flags.use_claw_server_rust)

    def test_rejects_non_boolean_flag_value(self):
        with tempfile.TemporaryDirectory() as tmp:
            config = Path(tmp) / "bos_build" / "config" / "build_flags.yaml"
            config.parent.mkdir(parents=True)
            config.write_text("use_claw_server_rust: rust\n")

            with self.assertRaisesRegex(ValueError, "use_claw_server_rust"):
                load_build_flags(Path(tmp))

    def test_context_helper_reuses_loaded_flags(self):
        ctx = SimpleNamespace(build_flags=BuildFlags(use_claw_server_rust=False))

        self.assertFalse(build_flags_for_context(ctx).use_claw_server_rust)


if __name__ == "__main__":
    unittest.main()
