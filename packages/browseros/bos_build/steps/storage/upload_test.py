#!/usr/bin/env python3
"""Tests for release artifact upload metadata helpers."""

import unittest
import tempfile
from pathlib import Path
from types import SimpleNamespace
from unittest import mock

from bos_build.core.context import ArtifactRegistry
from bos_build.steps.storage.upload import (
    _get_artifact_key,
    merge_release_metadata,
    upload_release_artifacts,
)


class UploadMetadataTest(unittest.TestCase):
    def test_linux_x64_artifacts_use_x64_keys(self) -> None:
        self.assertEqual(
            _get_artifact_key("BrowserOS_v1.2.3_x64.AppImage", "linux"),
            "x64_appimage",
        )
        self.assertEqual(
            _get_artifact_key("BrowserOS_v1.2.3_amd64.deb", "linux"),
            "x64_deb",
        )

    def test_linux_arm64_artifacts_use_arm64_keys(self) -> None:
        self.assertEqual(
            _get_artifact_key("BrowserOS_v1.2.3_arm64.AppImage", "linux"),
            "arm64_appimage",
        )
        self.assertEqual(
            _get_artifact_key("BrowserOS_v1.2.3_arm64.deb", "linux"),
            "arm64_deb",
        )
        self.assertEqual(
            _get_artifact_key("BrowserOS_v1.2.3_aarch64.deb", "linux"),
            "arm64_deb",
        )

    def test_windows_installer_artifacts_use_arch_keys(self) -> None:
        self.assertEqual(
            _get_artifact_key("BrowserOS_v1.2.3_x64_installer.exe", "win"),
            "x64_installer",
        )
        self.assertEqual(
            _get_artifact_key("BrowserOS_v1.2.3_x64_installer.zip", "win"),
            "x64_zip",
        )
        self.assertEqual(
            _get_artifact_key("BrowserOS_v1.2.3_arm64_installer.exe", "win"),
            "arm64_installer",
        )
        self.assertEqual(
            _get_artifact_key("BrowserOS_v1.2.3_arm64_installer.zip", "win"),
            "arm64_zip",
        )

    def test_upload_attaches_macos_dmg_signature_metadata_by_filename(self) -> None:
        with (
            tempfile.TemporaryDirectory() as tmp,
            mock.patch("bos_build.steps.storage.upload.BOTO3_AVAILABLE", True),
            mock.patch("bos_build.steps.storage.upload.IS_MACOS", lambda: True),
            mock.patch("bos_build.steps.storage.upload.IS_WINDOWS", lambda: False),
            mock.patch(
                "bos_build.steps.storage.upload.get_r2_client", return_value=object()
            ),
            mock.patch(
                "bos_build.steps.storage.upload.upload_file_to_r2",
                return_value=True,
            ),
        ):
            dist_dir = Path(tmp)
            dmg_name = "BrowserOS_v1.2.3_arm64.dmg"
            (dist_dir / dmg_name).write_bytes(b"dmg")
            ctx = SimpleNamespace(
                env=SimpleNamespace(
                    r2_bucket="browseros",
                    r2_cdn_base_url="https://cdn.browseros.com",
                    has_r2_config=lambda: True,
                ),
                artifact_registry=ArtifactRegistry(),
                product=SimpleNamespace(id="browseros", display_name="BrowserOS"),
                chromium_version="136.0.0.0",
                browseros_chromium_version="136.0.0.0.1",
                get_dist_dir=lambda: dist_dir,
                get_semantic_version=lambda: "1.2.3",
                get_sparkle_version=lambda: "10000.1.2.3",
                get_release_path=lambda platform: f"releases/browseros/1.2.3/{platform}/",
            )

            success, release = upload_release_artifacts(
                ctx,
                {dmg_name: {"sparkle_signature": "SIG==", "sparkle_length": 3}},
            )

        self.assertTrue(success)
        artifact = release["artifacts"]["arm64"]
        self.assertEqual(artifact["filename"], dmg_name)
        self.assertEqual(artifact["sparkle_signature"], "SIG==")
        self.assertEqual(artifact["sparkle_length"], 3)

    def test_merge_release_metadata_preserves_existing_artifacts(self) -> None:
        existing = {
            "platform": "linux",
            "version": "1.2.3",
            "build_date": "old",
            "artifacts": {
                "x64_appimage": {"filename": "BrowserOS_v1.2.3_x64.AppImage"},
                "x64_deb": {"filename": "BrowserOS_v1.2.3_amd64.deb"},
            },
        }
        new = {
            "platform": "linux",
            "version": "1.2.3",
            "build_date": "new",
            "artifacts": {
                "arm64_appimage": {"filename": "BrowserOS_v1.2.3_arm64.AppImage"},
                "arm64_deb": {"filename": "BrowserOS_v1.2.3_arm64.deb"},
            },
        }

        merged = merge_release_metadata(existing, new)

        self.assertEqual(merged["build_date"], "new")
        self.assertEqual(
            sorted(merged["artifacts"]),
            ["arm64_appimage", "arm64_deb", "x64_appimage", "x64_deb"],
        )

    def test_merge_release_metadata_overwrites_matching_artifact_keys(self) -> None:
        existing = {
            "platform": "linux",
            "version": "1.2.3",
            "artifacts": {
                "x64_appimage": {"filename": "old.AppImage", "size": 1},
            },
        }
        new = {
            "platform": "linux",
            "version": "1.2.3",
            "artifacts": {
                "x64_appimage": {"filename": "new.AppImage", "size": 2},
            },
        }

        merged = merge_release_metadata(existing, new)

        self.assertEqual(merged["artifacts"]["x64_appimage"]["filename"], "new.AppImage")
        self.assertEqual(merged["artifacts"]["x64_appimage"]["size"], 2)


if __name__ == "__main__":
    unittest.main()
