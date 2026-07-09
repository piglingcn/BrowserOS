#!/usr/bin/env python3
"""Human-editable build flags loaded from bos_build/config/build_flags.yaml."""

from dataclasses import dataclass
from pathlib import Path
from typing import Any

import yaml

from .paths import get_package_root

BUILD_FLAGS_CONFIG = Path("bos_build/config/build_flags.yaml")


@dataclass(frozen=True)
class BuildFlags:
    """Boolean switches that intentionally stay outside CLI/profile sprawl."""

    use_claw_server_rust: bool = True


def load_build_flags(root_dir: Path | None = None) -> BuildFlags:
    """Load build flags, defaulting missing values to their dataclass defaults."""
    root = Path(root_dir) if root_dir is not None else get_package_root()
    config_path = root / BUILD_FLAGS_CONFIG
    if not config_path.exists():
        return BuildFlags()

    with open(config_path, "r") as f:
        raw = yaml.safe_load(f) or {}

    if not isinstance(raw, dict):
        raise ValueError(f"Build flags config must be a YAML mapping: {config_path}")

    return BuildFlags(
        use_claw_server_rust=_optional_bool(
            raw,
            "use_claw_server_rust",
            BuildFlags.use_claw_server_rust,
            config_path,
        )
    )


def build_flags_for_context(context: Any) -> BuildFlags:
    """Return flags already loaded on a Context-like object, or load by root_dir."""
    flags = getattr(context, "build_flags", None)
    if isinstance(flags, BuildFlags):
        return flags
    return load_build_flags(getattr(context, "root_dir", None))


def _optional_bool(
    raw: dict[str, Any], key: str, default: bool, config_path: Path
) -> bool:
    value = raw.get(key, default)
    if not isinstance(value, bool):
        raise ValueError(
            f"{config_path}: {key} must be a boolean, got {type(value).__name__}"
        )
    return value
