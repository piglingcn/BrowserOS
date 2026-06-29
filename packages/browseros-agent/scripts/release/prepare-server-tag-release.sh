#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat >&2 <<'EOF'
Usage: prepare-server-tag-release.sh --tag <agent-server/vX.Y.Z> --default-branch <branch> [--agent-root <path>] [--bump-script <path>] [--remote <name>]
EOF
}

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
agent_root="$(cd "$script_dir/../.." && pwd -P)"
bump_script="$agent_root/../browseros/build/scripts/bump_server_version.py"
tag=""
default_branch=""
remote="origin"

while [ "$#" -gt 0 ]; do
  case "$1" in
    --tag)
      tag="${2:-}"
      shift 2
      ;;
    --default-branch)
      default_branch="${2:-}"
      shift 2
      ;;
    --agent-root)
      agent_root="$(cd "${2:-}" && pwd -P)"
      shift 2
      ;;
    --bump-script)
      bump_script="$(cd "$(dirname "${2:-}")" && pwd -P)/$(basename "${2:-}")"
      shift 2
      ;;
    --remote)
      remote="${2:-}"
      shift 2
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

if [ -z "$tag" ] || [ -z "$default_branch" ]; then
  usage
  exit 2
fi

resolver="$script_dir/resolve-component-release.sh"

# Runs the component resolver from the package root so path detection matches Actions.
run_resolver() {
  (
    cd "$agent_root"
    "$resolver" \
      --component agent-server \
      --tag "$tag" \
      --default-branch "$default_branch" \
      "$@"
  )
}

# Extracts the resolver fields needed before deciding whether a tag repair is allowed.
parse_release_output() {
  while IFS='=' read -r key value; do
    [ -n "$key" ] || continue
    case "$key" in
      version) version="$value" ;;
      package_version) package_version="$value" ;;
      package_version_matches) package_version_matches="$value" ;;
      release_sha) release_sha="$value" ;;
    esac
  done <<< "$1"
}

# Seeds the Actions bot identity when the checkout has no commit author configured.
ensure_git_identity() {
  if ! git -C "$agent_root" config user.name >/dev/null; then
    git -C "$agent_root" config user.name "github-actions[bot]"
  fi
  if ! git -C "$agent_root" config user.email >/dev/null; then
    git -C "$agent_root" config user.email "41898282+github-actions[bot]@users.noreply.github.com"
  fi
}

# Compares strict release versions before allowing the workflow to edit package files.
version_gt() {
  local left_major left_minor left_patch right_major right_minor right_patch
  IFS=. read -r left_major left_minor left_patch <<< "$1"
  IFS=. read -r right_major right_minor right_patch <<< "$2"

  for part in "$left_major" "$left_minor" "$left_patch" "$right_major" "$right_minor" "$right_patch"; do
    [[ "$part" =~ ^[0-9]+$ ]] || return 1
  done

  if [ "$left_major" -ne "$right_major" ]; then
    [ "$left_major" -gt "$right_major" ]
    return
  fi
  if [ "$left_minor" -ne "$right_minor" ]; then
    [ "$left_minor" -gt "$right_minor" ]
    return
  fi
  [ "$left_patch" -gt "$right_patch" ]
}

preflight_output="$(unset GITHUB_OUTPUT; run_resolver --allow-package-version-mismatch)"

version=""
package_version=""
package_version_matches=""
release_sha=""
parse_release_output "$preflight_output"

if [ "$package_version_matches" = "true" ]; then
  run_resolver
  exit 0
fi

if ! version_gt "$version" "$package_version"; then
  echo "Auto-bump requires tag version $version to be greater than package version $package_version" >&2
  exit 1
fi

git -C "$agent_root" fetch "$remote" "$default_branch:refs/remotes/$remote/$default_branch" --no-tags
default_sha="$(git -C "$agent_root" rev-parse "$remote/$default_branch")"

if [ "$release_sha" != "$default_sha" ]; then
  echo "Auto-bump requires $tag to point at current $remote/$default_branch ($default_sha), got $release_sha" >&2
  exit 1
fi

ensure_git_identity

git -C "$agent_root" switch -C "$default_branch" "$remote/$default_branch"
python3 "$bump_script" --agent-root "$agent_root" --set "$version" >/dev/null

if git -C "$agent_root" diff --quiet -- apps/server/package.json bun.lock; then
  echo "No server version changes produced for $tag" >&2
  exit 1
fi

git -C "$agent_root" add apps/server/package.json bun.lock
git -C "$agent_root" commit -m "chore: bump server version to $version"
git -C "$agent_root" tag -f -a "$tag" -m "agent-server v$version"
git -C "$agent_root" push --atomic "$remote" \
  "HEAD:refs/heads/$default_branch" \
  "+refs/tags/$tag:refs/tags/$tag"
git -C "$agent_root" fetch "$remote" "$default_branch:refs/remotes/$remote/$default_branch" --no-tags

run_resolver
