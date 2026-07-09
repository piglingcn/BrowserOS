#!/usr/bin/env python3
"""Tests for features.yaml IO helpers."""

import tempfile
import unittest
from pathlib import Path
from types import SimpleNamespace
from typing import cast

from bos_build.core.context import Context
from bos_build.patchkit.features_io import (
    add_files_to_feature,
    load_features_yaml,
    save_features_yaml,
)


class FeaturesIOTest(unittest.TestCase):
    def setUp(self):
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        self.features_file = Path(tmp.name) / "features.yaml"

    def _ctx(self) -> Context:
        return cast(
            Context,
            SimpleNamespace(get_features_yaml_path=lambda: self.features_file),
        )

    def test_round_trip(self):
        data = {
            "version": "1.0",
            "features": {"foo": {"description": "d", "files": ["a.cc"]}},
        }
        save_features_yaml(self.features_file, data)
        self.assertEqual(load_features_yaml(self.features_file), data)

    def test_add_files_creates_feature(self):
        save_features_yaml(self.features_file, {"version": "1.0", "features": {}})

        added = add_files_to_feature(self._ctx(), "foo", "desc", ["a.cc", "b.cc"])

        self.assertEqual(added, 2)
        data = load_features_yaml(self.features_file)
        self.assertEqual(data["features"]["foo"]["files"], ["a.cc", "b.cc"])
        self.assertEqual(data["features"]["foo"]["description"], "desc")

    def test_add_files_skips_duplicates(self):
        save_features_yaml(
            self.features_file,
            {
                "version": "1.0",
                "features": {"foo": {"description": "d", "files": ["a.cc"]}},
            },
        )

        added = add_files_to_feature(self._ctx(), "foo", "d", ["a.cc", "b.cc"])

        self.assertEqual(added, 1)
        data = load_features_yaml(self.features_file)
        self.assertEqual(sorted(data["features"]["foo"]["files"]), ["a.cc", "b.cc"])


if __name__ == "__main__":
    unittest.main()
