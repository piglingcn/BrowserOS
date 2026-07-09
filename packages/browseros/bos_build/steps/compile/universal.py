#!/usr/bin/env python3
"""merge_universal step: fold the per-arch release builds into one app.

Planner-emitted as run 3 of the universal pipeline (core/planner.
plan_runs): the two prior runs leave arm64 and x64 apps at their
deterministic out dirs, so inputs derive from product+arch — no
cross-run artifact plumbing. The merged app lands exactly where
ctx(universal).get_app_path() resolves; sign/package/upload then treat
it like any other build.
"""

from pathlib import Path

from ...core.context import Context
from ...core.step import Step, ValidationError, step
from ..package.merge import merge_architectures

UNIVERSAL_ARCHITECTURES = ("arm64", "x64")


def _universalizer_script(ctx: Context) -> Path:
    return ctx.root_dir / "bos_build/steps/package/universalizer_patched.py"


def _arch_app_path(ctx: Context, arch: str) -> Path:
    """Sibling per-arch app path via Context, single-sourcing the out-dir scheme."""
    return Context(
        root_dir=ctx.root_dir,
        chromium_src=ctx.chromium_src,
        architecture=arch,
        build_type=ctx.build_type,
        product=ctx.product,
    ).get_app_path()


@step("merge_universal", phase="build", platforms=("macos",), optional=True)
class MergeUniversalModule(Step):
    produces = ["built_app"]
    requires = []
    description = "Merge arm64 + x64 release builds into a universal app"

    def preflight(self, ctx: Context) -> None:
        script = _universalizer_script(ctx)
        if not script.exists():
            raise ValidationError(f"Universalizer script not found: {script}")

    def validate(self, ctx: Context) -> None:
        # Input apps are produced by the previous runs of the universal
        # pipeline, so they are checked just-in-time here, not in preflight.
        if ctx.architecture != "universal":
            raise ValidationError(
                f"merge_universal needs a universal context, got '{ctx.architecture}'"
            )
        for arch in UNIVERSAL_ARCHITECTURES:
            app = _arch_app_path(ctx, arch)
            if not app.exists():
                raise ValidationError(f"{arch} app not found (build it first): {app}")

    def execute(self, ctx: Context) -> None:
        arm64_app, x64_app = (
            _arch_app_path(ctx, arch) for arch in UNIVERSAL_ARCHITECTURES
        )
        output = ctx.get_app_path()
        if not merge_architectures(
            arch1_path=arm64_app,
            arch2_path=x64_app,
            output_path=output,
            universalizer_script=_universalizer_script(ctx),
        ):
            raise RuntimeError("Failed to merge architectures into universal app")
        ctx.artifact_registry.add("built_app", output)
