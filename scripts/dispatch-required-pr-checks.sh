#!/usr/bin/env bash

set -euo pipefail

if [ "$#" -ne 1 ]; then
  echo "usage: $0 <ref>" >&2
  exit 1
fi

if [ -z "${GH_TOKEN:-}" ]; then
  echo "GH_TOKEN is required" >&2
  exit 1
fi

if [ -z "${GH_REPO:-}" ]; then
  echo "GH_REPO is required" >&2
  exit 1
fi

ref="$1"
workflows=(
  "ci.yaml"
  "dependency-review.yaml"
  "govulncheck.yaml"
  "lychee.yaml"
  "codeql.yaml"
)

for workflow in "${workflows[@]}"; do
  echo "::group::Dispatch ${workflow} on ${ref}"
  gh api \
    -X POST \
    -H "Accept: application/vnd.github+json" \
    "repos/${GH_REPO}/actions/workflows/${workflow}/dispatches" \
    -f ref="${ref}"
  echo "::notice::dispatched ${workflow} for ${ref}"
  echo "::endgroup::"
done
