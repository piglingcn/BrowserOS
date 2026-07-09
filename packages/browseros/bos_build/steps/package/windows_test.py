#!/usr/bin/env python3
"""Tests for the Windows packaging module (autoninja routing and validate())."""

import tempfile
import unittest
from pathlib import Path
from types import SimpleNamespace
from typing import cast
from unittest import mock

from . import windows
from ..compile import standard
from ...core.context import Context
from ...core.step import ValidationError


class BuildMiniInstallerTest(unittest.TestCase):
    def test_routes_through_shared_argv_builder_with_override(self):
        ctx = cast(
            Context,
            SimpleNamespace(
                out_dir="out/Default_x64", chromium_src=Path("/tmp/chromium-src")
            ),
        )
        with (
            mock.patch.object(windows, "run_command") as run_cmd,
            mock.patch.object(standard, "IS_WINDOWS", return_value=False),
            mock.patch("os.chdir"),
            mock.patch("os.getcwd", return_value="/anywhere"),
            mock.patch.dict("os.environ", {"BROWSEROS_NINJA_JOBS": "8"}, clear=True),
        ):
            result = windows.build_mini_installer(ctx)
        run_cmd.assert_called_once_with(
            ["autoninja", "-C", "out/Default_x64", "-j", "8", "setup", "mini_installer"]
        )
        # Artifacts were never produced (run_command is mocked), so it reports failure.
        self.assertFalse(result)


class WindowsPackageModuleValidateTest(unittest.TestCase):
    def setUp(self):
        self._tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self._tmp.cleanup)
        self.chromium_src = Path(self._tmp.name) / "chromium" / "src"
        self.out_dir = "out/Default_x64"
        self.build_output_dir = self.chromium_src / self.out_dir
        self.build_output_dir.mkdir(parents=True)
        self.ctx = cast(
            Context,
            SimpleNamespace(chromium_src=self.chromium_src, out_dir=self.out_dir),
        )

    def _touch_output(self, name: str) -> Path:
        path = self.build_output_dir / name
        path.write_text("")
        return path

    def test_validate_raises_when_winsparkle_dll_missing(self):
        self._touch_output("mini_installer.exe")
        winsparkle_path = self.build_output_dir / "WinSparkle.dll"

        with mock.patch.object(windows, "IS_WINDOWS", return_value=True):
            with self.assertRaises(ValidationError) as raised:
                windows.WindowsPackageModule().validate(self.ctx)

        message = str(raised.exception)
        self.assertIn("WinSparkle.dll", message)
        self.assertIn(str(winsparkle_path), message)
        self.assertIn("auto-update", message)

    def test_validate_passes_when_installer_and_winsparkle_dll_exist(self):
        self._touch_output("mini_installer.exe")
        self._touch_output("WinSparkle.dll")

        with mock.patch.object(windows, "IS_WINDOWS", return_value=True):
            windows.WindowsPackageModule().validate(self.ctx)

    def test_validate_raises_when_mini_installer_missing(self):
        self._touch_output("WinSparkle.dll")
        mini_installer_path = self.build_output_dir / "mini_installer.exe"

        with mock.patch.object(windows, "IS_WINDOWS", return_value=True):
            with self.assertRaises(ValidationError) as raised:
                windows.WindowsPackageModule().validate(self.ctx)

        self.assertEqual(
            str(raised.exception), f"mini_installer.exe not found: {mini_installer_path}"
        )

    def test_validate_raises_when_not_windows(self):
        with mock.patch.object(windows, "IS_WINDOWS", return_value=False):
            with self.assertRaisesRegex(
                ValidationError, "Windows packaging requires Windows"
            ):
                windows.WindowsPackageModule().validate(self.ctx)


if __name__ == "__main__":
    unittest.main()
