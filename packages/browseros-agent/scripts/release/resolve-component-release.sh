#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat >&2 <<'EOF'
Usage: resolve-component-release.sh --component agent-extension|agent-server --tag <tag> --default-branch <branch> [--allow-package-version-mismatch]
EOF
}

component=""
tag=""
default_branch=""
allow_package_version_mismatch=false

while [ "$#" -gt 0 ]; do
  case "$1" in
    --component)
      component="${2:-}"
      shift 2
      ;;
    --tag)
      tag="${2:-}"
      shift 2
      ;;
    --default-branch)
      default_branch="${2:-}"
      shift 2
      ;;
    --allow-package-version-mismatch)
      allow_package_version_mismatch=true
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "Unknown argument: $1" >&2
      usage
      exit 2
      ;;
  esac
done

if [ -z "$component" ] || [ -z "$tag" ] || [ -z "$default_branch" ]; then
  usage
  exit 2
fi

case "$component" in
  agent-extension)
    new_prefix="agent-extension/v"
    legacy_prefix="agent-extension-v"
    package_json="apps/app/package.json"
    ;;
  agent-server)
    new_prefix="agent-server/v"
    legacy_prefix="browseros-server-v"
    package_json="apps/server/package.json"
    ;;
  *)
    echo "Unsupported component: $component" >&2
    usage
    exit 2
    ;;
esac

is_semver() {
  [[ "$1" =~ ^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]]
}

extract_version() {
  local candidate
  case "$1" in
    "$new_prefix"*)
      candidate="${1#"$new_prefix"}"
      ;;
    "$legacy_prefix"*)
      candidate="${1#"$legacy_prefix"}"
      ;;
    *)
      return 1
      ;;
  esac

  if is_semver "$candidate"; then
    printf '%s\n' "$candidate"
    return 0
  fi

  return 1
}

version_gt() {
  local left_major left_minor left_patch right_major right_minor right_patch
  IFS=. read -r left_major left_minor left_patch <<< "$1"
  IFS=. read -r right_major right_minor right_patch <<< "$2"

  if [ "$left_major" -ne "$right_major" ]; then
    if [ "$left_major" -gt "$right_major" ]; then
      return 0
    fi
    return 1
  fi
  if [ "$left_minor" -ne "$right_minor" ]; then
    if [ "$left_minor" -gt "$right_minor" ]; then
      return 0
    fi
    return 1
  fi
  if [ "$left_patch" -gt "$right_patch" ]; then
    return 0
  fi
  return 1
}

emit() {
  printf '%s=%s\n' "$1" "$2"
  if [ -n "${GITHUB_OUTPUT:-}" ]; then
    printf '%s=%s\n' "$1" "$2" >> "$GITHUB_OUTPUT"
  fi
}

tag_version="$(extract_version "$tag" || true)"
if [ -z "$tag_version" ] || [[ "$tag" != "$new_prefix"* ]]; then
  echo "Expected $component tag like ${new_prefix}X.Y.Z, got: $tag" >&2
  exit 1
fi

tag_type="$(git cat-file -t "refs/tags/$tag" 2>/dev/null || true)"
if [ -z "$tag_type" ]; then
  echo "Tag does not exist in this checkout: $tag" >&2
  exit 1
fi
if [ "$tag_type" != "tag" ]; then
  echo "Tag $tag must be an annotated tag" >&2
  exit 1
fi

release_sha="$(git rev-list -n 1 "$tag")"

git_root="$(git rev-parse --show-toplevel)"
git_root="$(cd "$git_root" && pwd -P)"
current_dir="$(pwd -P)"
git_package_json="$package_json"
browseros_agent_package_json="packages/browseros-agent/$package_json"

# Git object paths are repository-root-relative, but release jobs run from the package checkout.
case "$current_dir" in
  "$git_root")
    ;;
  "$git_root"/*)
    git_package_json="${current_dir#"$git_root"/}/$package_json"
    ;;
esac

git_package_json_candidates=("$git_package_json")
if [ "$git_package_json" != "$package_json" ]; then
  git_package_json_candidates+=("$package_json")
fi
if [ "$git_package_json" != "$browseros_agent_package_json" ]; then
  git_package_json_candidates+=("$browseros_agent_package_json")
fi

found_package_blob=false
for candidate in "${git_package_json_candidates[@]}"; do
  if package_blob="$(git show "$release_sha:$candidate" 2>/dev/null)"; then
    git_package_json="$candidate"
    found_package_blob=true
    break
  fi
done

if [ "$found_package_blob" != "true" ]; then
  echo "Could not read $package_json version from $tag ($release_sha)" >&2
  exit 1
fi

if ! package_version="$(
  printf '%s\n' "$package_blob" | python3 -c '
import json
import sys

try:
    print(json.load(sys.stdin)["version"])
except Exception as exc:
    print(f"{type(exc).__name__}: {exc}", file=sys.stderr)
    sys.exit(1)
'
)"; then
  echo "Could not read $package_json version from $tag ($release_sha)" >&2
  exit 1
fi

package_version_matches=true
if [ "$package_version" != "$tag_version" ]; then
  package_version_matches=false
  if [ "$allow_package_version_mismatch" != "true" ]; then
    echo "Tag version $tag_version does not match $package_json version $package_version" >&2
    exit 1
  fi
fi

default_ref=""
if git rev-parse --verify --quiet "origin/$default_branch^{commit}" >/dev/null; then
  default_ref="origin/$default_branch"
elif git rev-parse --verify --quiet "$default_branch^{commit}" >/dev/null; then
  default_ref="$default_branch"
else
  git fetch origin "$default_branch:refs/remotes/origin/$default_branch" --no-tags
  default_ref="origin/$default_branch"
fi

if ! git merge-base --is-ancestor "$release_sha" "$default_ref"; then
  echo "Tagged commit $release_sha is not reachable from $default_ref" >&2
  exit 1
fi

latest_version=""
latest_tag=""
duplicate_tag=""

while IFS= read -r existing_tag; do
  [ -n "$existing_tag" ] || continue
  [ "$existing_tag" != "$tag" ] || continue

  existing_version="$(extract_version "$existing_tag" || true)"
  [ -n "$existing_version" ] || continue

  if [ "$existing_version" = "$tag_version" ]; then
    duplicate_tag="$existing_tag"
    break
  fi

  if [ -z "$latest_version" ] || version_gt "$existing_version" "$latest_version"; then
    latest_version="$existing_version"
    latest_tag="$existing_tag"
  fi
done < <(
  {
    git tag -l "${legacy_prefix}*"
    git tag -l "${new_prefix}*"
  } | sort -u
)

if [ -n "$duplicate_tag" ]; then
  echo "Release version $tag_version already exists as tag $duplicate_tag" >&2
  exit 1
fi

if [ -n "$latest_version" ] && ! version_gt "$tag_version" "$latest_version"; then
  echo "Release version $tag_version must be greater than latest existing $component version $latest_version ($latest_tag)" >&2
  exit 1
fi

emit version "$tag_version"
emit package_version "$package_version"
emit package_version_matches "$package_version_matches"
emit tag "$tag"
emit release_sha "$release_sha"
emit previous_tag "$latest_tag"
