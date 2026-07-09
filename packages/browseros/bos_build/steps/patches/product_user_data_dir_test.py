#!/usr/bin/env python3
"""Tests for product user-data directory patches."""

import re
import unittest

from ...lib.paths import get_package_root


PATCHES = get_package_root() / "chromium_patches"


def _patch(relative_path: str) -> str:
    return (PATCHES / relative_path).read_text()


class ProductUserDataDirPatchTest(unittest.TestCase):
    def test_mac_profile_root_comes_from_browseros_product_gn_arg(self) -> None:
        build = _patch("chrome/BUILD.gn")
        plist = _patch("chrome/app/app-Info.plist")
        buildflags = _patch("chrome/browser/browseros/buildflags.gni")

        self.assertIn(
            '"BROWSEROS_PRODUCT_DIR_NAME=$browseros_product_dir_name"', build
        )
        self.assertRegex(
            plist,
            re.compile(
                r"\+\t<key>CrProductDirName</key>\n"
                r"\+\t<string>\$\{BROWSEROS_PRODUCT_DIR_NAME\}</string>"
            ),
        )
        self.assertRegex(
            buildflags,
            re.compile(
                r'\+if \(browseros_product_browserclaw\) \{\n'
                r'\+  browseros_product_dir_name = "BrowserClaw"\n'
                r'\+\} else \{\n'
                r'\+  browseros_product_dir_name = "BrowserOS"\n'
                r'\+\}'
            ),
        )

    def test_linux_profile_roots_are_product_specific(self) -> None:
        linux_paths = _patch("chrome/common/chrome_paths_linux.cc")

        self.assertRegex(
            linux_paths,
            re.compile(
                r"\+#elif BUILDFLAG\(BROWSEROS_PRODUCT_BROWSERCLAW\)\n"
                r'\+  std::string data_dir_basename = "browser-claw";\n'
                r" #else\n"
                r'-  std::string data_dir_basename = "chromium";\n'
                r'\+  std::string data_dir_basename = "browser-os";'
            ),
        )

    def test_windows_profile_roots_are_product_specific(self) -> None:
        install_modes = _patch("chrome/install_static/chromium_install_modes.h")

        self.assertRegex(
            install_modes,
            re.compile(
                r"\+#if BUILDFLAG\(BROWSEROS_PRODUCT_BROWSERCLAW\)\n"
                r'\+inline constexpr wchar_t kProductPathName\[\] = L"BrowserClaw";\n'
                r"\+#else\n"
                r'\+inline constexpr wchar_t kProductPathName\[\] = L"BrowserOS";\n'
                r"\+#endif"
            ),
        )


if __name__ == "__main__":
    unittest.main()
