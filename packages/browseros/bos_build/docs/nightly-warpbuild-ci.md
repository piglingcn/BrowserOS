# Nightly WarpBuild Release Builds

`.github/workflows/nightly-release.yml` (`Nightly: All-Platform Browser
(unsigned)`) builds UNSIGNED release artifacts for Linux x64, Windows x64, and
macOS arm64 every night on WarpBuild cloud runners, uploads them to the Actions
run, and refreshes a rolling `nightly` prerelease on GitHub. Its per-platform
build job delegates to the reusable Chromium build workflow in
`.github/workflows/build-browseros.yml`, so the WarpBuild checkout/cache/build
recipe is shared with the release workflows.
It complements the signed self-hosted macOS product nightlies in
`nightly-browseros.yml` and `nightly-browserclaw.yml`. Those dedicated Mac Mini
workflows are the signed daily-test path for macOS arm64.

## Runners

| Platform | Label | Specs | Disk |
| --- | --- | --- | --- |
| Linux x64 | `warp-ubuntu-2204-x64-32x` | 32 vCPU / 128 GB | 256 GB |
| Windows x64 | `warp-windows-2025-x64-32x` | 32 vCPU / 128 GB | 256 GB |
| macOS arm64 | `warp-macos-26-arm64-12x` | M4 Pro, 12 vCPU / 44 GB | 500 GB |

There is no 32-core macOS tier; 12x is WarpBuild's largest Mac. The macOS
image version must satisfy the chromium pin's SDK requirement — check
`build/config/mac/mac_sdk.gni` (`mac_sdk_official_version`) in the pinned
tree when bumping `CHROMIUM_VERSION`; chromium 148 needs the macOS 26 SDK,
and the macOS 15 image (Xcode 16.4 / SDK 15.5) fails compiling
`skia_utils_mac.mm` (`kCGImageByteOrder32Host` only exists in SDK 26).
WarpBuild runners register as self-hosted, so GitHub's 6-hour hosted-job
cap does not apply — but `timeout-minutes` must be set explicitly (the
implicit default is 360). Disk is comfortable on all three tiers — the
~60-75 GB checkout + ~25-40 GB out dir leave ample headroom. The workflow
prints `df -h` after each build. Specs above come from the account's runner catalog
(app.warpbuild.com → Runners), which is the source of truth for labels
and sizes; WarpBuild's public docs pages can lag it.

## One-time setup (WarpBuild)

The `warpbuildbot` GitHub app is installed org-wide on `browseros-ai`
(since 2026-06-11). Two more things must be true before any `warp-*` job
leaves `queued`:

1. **The org must allow self-hosted runners on public repos.** WarpBuild
   runners register as org-level self-hosted runners, and GitHub blocks
   those on public repositories by default
   (https://www.warpbuild.com/docs/ci/public-repos). BrowserOS is public,
   so an org admin must check: Organization Settings → Actions → Runner
   groups → Default → "Allow public repositories". Via API (needs
   `admin:org` scope):

   ```bash
   gh auth refresh -h github.com -s admin:org
   gh api orgs/browseros-ai/actions/runner-groups \
     --jq '.runner_groups[] | {id, name, allows_public_repositories}'
   gh api -X PATCH "orgs/browseros-ai/actions/runner-groups/<id>" \
     -F allows_public_repositories=true
   ```

   Before flipping the toggle, check what else lives in that group — it
   widens exposure for every runner in it:

   ```bash
   gh api "orgs/browseros-ai/actions/runner-groups/<id>/runners" \
     --jq '.runners[] | {name, status, labels: [.labels[].name]}'
   ```

   Expect only ephemeral `warp-*` runners (usually none while idle). The
   signed-nightly Mac (`browseros-builder`) is registered at the repo
   level, so this org-group toggle does not change its exposure. If the
   group ever holds other persistent org-level runners, give WarpBuild a
   dedicated runner group instead of widening Default.

   Done for `browseros-ai` on 2026-06-13 — pickup verified live (a
   queued job was claimed within ~60 s of dispatch).

2. **The WarpBuild org must be active**: sign in at
   https://app.warpbuild.com/, confirm the `browseros-ai` connection and
   that billing/credits are set up — runners are not provisioned without
   an active account.

Smoke test after changing either:
`gh workflow run nightly-release.yml -f platforms=linux`, then watch the build
job leave `queued` within ~5 minutes (`gh run watch`).

## Per-night pipeline (per platform)

`nightly-release.yml` resolves the platform matrix and then calls
`.github/workflows/build-browseros.yml` once per selected platform with
`product=browseros`, `profile=nightly-ci`, `sign=false`, `upload=false`, and
the historical artifact name `browseros-nightly-<platform>-<arch>`. The
reusable workflow performs the per-platform recipe:

1. `actions/checkout` + `astral-sh/setup-uv`.
2. Restore the pinned chromium checkout from cache (see below).
3. `browseros source ensure --step checkout` — ensures depot_tools and
   `src` at the tag from `packages/browseros/CHROMIUM_VERSION`. No-op when
   the cache is warm and the pin unchanged.
4. `uv run browseros build --modules clean ...` — the standard clean module
   resets the tree (it also deletes hook-managed toolchains like
   `third_party/llvm-build`, which the next step restores).
5. `browseros source ensure --step sync` — `gclient sync -D
   --no-history --shallow`, exactly what the git_setup module runs.
6. Save the cache (only when the restore missed, i.e. first run per pin).
7. `uv run browseros build --profile nightly-ci --arch <arch>
   --chromium-src .../src`.
8. Upload artifacts (14-day retention) using the nightly artifact name; a
   follow-up job in `nightly-release.yml` recreates the rolling `nightly`
   prerelease for scheduled main runs.

The `nightly-ci` profile is the release preset minus `clean`/
`git_setup` (steps 4-5 replace them), minus `sign_*`/`upload`. It keeps
CDN-pinned bundled extensions because WarpBuild images do not provide the
system Chrome binary required for local CRX packing. Why not run `git_setup`
as-is: it does `git fetch --tags`, which on the shallow CI clone would pull
objects for all ~70k chromium tags; the script instead fetches exactly the
pinned tag at depth 2. On Windows the new `mini_installer` module builds the
installer that `sign_windows` would otherwise build.

## Caching strategy

Cache key: `chromium-src-<platform>-<arch>-v1-<CHROMIUM_VERSION>`. Contents:
the whole gclient root (depot_tools, `.gclient`, post-sync `src`) captured
immediately after `gclient sync`, before patches and before any `out/` dir
exists — pristine and deterministic. The pin changes rarely, so steady state
is one cold sync per chromium bump per platform.

- **Linux / macOS — WarpCache** (`WarpBuilds/cache@v1`): drop-in for
  `actions/cache` with no size cap (entries expire 7 days after last use;
  storage $0.20/GB-month). Restore-keys fall back to the previous pin's
  cache, then the script fast-forwards `src` with a single-tag fetch.
- **Windows — R2 tarball** (`browseros source cache`): WarpCache does not
  support Windows runners and `actions/cache` caps at 10 GB/repo. The tree
  is zstd-tarred (~25-30 GB) into `ci-cache/chromium/` in the existing R2
  bucket using the same `R2_*` secrets the build already needs for
  `download_resources`. R2 has zero egress fees. Missing credentials or a
  cache miss degrade to a cold checkout, never a failure.

Expected timings (32-core linux/windows, M4 Pro mac):

| Phase | Cold (first run / pin bump) | Warm |
| --- | --- | --- |
| Checkout + sync | 40-70 min | restore 3-10 min + sync 5-15 min |
| Compile + package | 2.5-6 h (per platform) | same — out/ is rebuilt nightly |
| Total | ~4-7 h | ~3-6 h |

The compile dominates either way; the cache removes the checkout cost and
network flakiness. Toolchains deleted by `clean` (~2-4 GB) are re-fetched by
hooks each night — accepted, matches the maintainer's local flow.

Cost ballpark at WarpBuild list prices: linux 32x $3.84/h, windows 32x
$7.68/h, mac 12x $9.60/h → roughly $60-120 per full nightly, plus ~$12/month
cache storage.

### Future optimizations (not yet wired)

- **Snapshot runners (Linux only)**: boot from a disk image with the
  checkout baked in (`runs-on: "warp-ubuntu-2204-x64-32x;snapshot.key=..."`
  + `WarpBuilds/snapshot-save`). Kills even the cache-restore minutes, but
  snapshots expire after 15 days and need a bake/boot split; revisit once
  the cache flow is proven.
- **Compiler cache (sccache/ccache via `cc_wrapper`)** in a CI gn flags
  variant: nightly sources are nearly identical night-to-night, so this is
  the lever that could cut warm builds to well under an hour.
- **Linux arm64** via `architecture: [x64, arm64]` in the CI config once
  the x64 lane is green (sysroot bootstrap already handled by the modules).

## Signing later (placeholders)

The workflow leaves named-but-unused secret placeholders documented next to
the build step. To enable signing:

- **macOS**: enable signing on
  the macOS build invocation (`--sign`), provide `MACOS_CERTIFICATE_P12` +
  `MACOS_CERTIFICATE_PWD` (import into a temporary CI keychain in a step
  before the build), `MACOS_CERTIFICATE_NAME`, `MACOS_KEYCHAIN_PASSWORD`,
  `PROD_MACOS_NOTARIZATION_APPLE_ID/_TEAM_ID/_PWD`, `SPARKLE_PRIVATE_KEY`.
- **Windows**: enable signing on
  the Windows build invocation (`--sign`), install SSL.com CodeSignTool in a step, set
  `CODE_SIGN_TOOL_PATH` and `ESIGNER_USERNAME/_PASSWORD/_TOTP_SECRET`
  (+ `ESIGNER_CREDENTIAL_ID`).
- **Linux**: unsigned by design (no sign module in the release config).

## Operating it

```bash
# Full run on all platforms (no prerelease update):
gh workflow run nightly-release.yml

# One platform while iterating:
gh workflow run nightly-release.yml -f platforms=linux

# Manual run that also refreshes the rolling prerelease (main only):
gh workflow run nightly-release.yml -f publish_nightly=true
```

The first run per platform is the cache warm-up; expect cold timings. If a
pin bump lands, the next night is cold again for that version. To force a
fresh checkout, bump the `v1` in the cache key (workflow) — for Windows also
delete the old object under `ci-cache/chromium/` in R2.

## Troubleshooting: jobs stuck in `queued`

A job no runner ever picked up shows `runner_id: 0` and empty steps:

```bash
gh run view <run-id> --json jobs --jq '.jobs[] | {name, status}'
gh api repos/browseros-ai/BrowserOS/actions/jobs/<job-id> \
  --jq '{status, runner_id, runner_name, labels}'
```

Causes, in the order to check:

1. **Runner group blocks public repos** — see one-time setup above. This
   stalls all platforms at once.
2. **Label not in the account's runner catalog** — the canonical list is
   the Runners page at https://app.warpbuild.com/ (the public docs lag
   it: in 2026-06 the preinstalled-software page omitted the Windows
   Server 2025 images the catalog already offered). An unsupported label
   queues forever; WarpBuild reports no error back to GitHub.
3. **WarpBuild account** — org connection or billing lapsed
   (https://app.warpbuild.com/).
4. **WarpBuild capacity or incident** — rare; check their dashboard.

Mechanics worth knowing:

- GitHub discards self-hosted jobs queued for more than 24h, and the
  workflow's `nightly-release` concurrency group
  (`cancel-in-progress: false`) makes the next run wait (newer pending
  runs supersede older pending ones) — one stuck night delays the next
  by a full day (runs 27367077749 → 27407228486 did exactly this). The `queue-watchdog` job therefore steps in at the
  20-minute mark: it cancels the run when no build job is actually
  running (everything stuck in queue or already finished), and fails
  loudly without cancelling while any build is in progress. In that
  mixed case, cancel the run manually once the live builds finish — a
  still-queued job otherwise pins the group for up to 24h with no
  watcher left.
- Fixing the root cause does not revive already-queued jobs: WarpBuild
  provisions on the `workflow_job.queued` webhook, which has already
  fired. Cancel the stuck run and re-dispatch.
- A job that IS picked up but dies in "Set up job" within seconds with
  `Unable to resolve action <owner>/<name>@vN` has nothing to do with
  WarpBuild: the floating major tag does not exist upstream (e.g.
  astral-sh/setup-uv publishes v8.x.y releases but no `v8` tag). Pin an
  exact existing version.
