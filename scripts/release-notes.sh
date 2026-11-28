#!/usr/bin/env bash
set -euo pipefail

commit="$(git rev-parse --verify "$1^{commit}")"
repository_url="${2%/}"
push_before="${3:-}"

# Only use a tag on this branch's history, and exclude tags on the current commit.
previous_tag="$(git describe --first-parent --tags --abbrev=0 "${commit}^" 2>/dev/null || true)"
base=""
if [[ -n "$previous_tag" ]]; then
  base="$(git rev-parse --verify "refs/tags/${previous_tag}^{commit}")"
elif [[ "$push_before" =~ ^[0-9a-f]{40}$ && "$push_before" != "$commit" ]] &&
  git merge-base --is-ancestor "$push_before" "$commit" 2>/dev/null; then
  base="$push_before"
fi

printf '## 本次更新\n\n'
commit_format="- %s ([%h]($repository_url/commit/%H))"
if [[ -n "$base" ]]; then
  git log --format="$commit_format" "$base..$commit"
  printf '\n[查看完整差异](%s/compare/%s...%s)\n' "$repository_url" "$base" "$commit"
else
  git log --max-count=50 --format="$commit_format" "$commit"
  printf '\n[查看完整提交历史](%s/commits/%s)\n' "$repository_url" "$commit"
fi
