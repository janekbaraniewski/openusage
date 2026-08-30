#!/usr/bin/env bash
# Copy Antigravity workflow template into a target project directory.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TEMPLATE_DIR="$SCRIPT_DIR/template"

usage() {
	echo "Usage: $0 <target-project-directory>" >&2
	echo "Copies .agents/ and AGENTS.md from the workflow kit template." >&2
	exit 1
}

[[ $# -eq 1 ]] || usage
TARGET="$(cd "$1" && pwd)"

if [[ ! -d "$TEMPLATE_DIR/.agents" ]]; then
	echo "error: template not found at $TEMPLATE_DIR" >&2
	exit 1
fi

mkdir -p "$TARGET"
cp "$TEMPLATE_DIR/AGENTS.md" "$TARGET/AGENTS.md"
cp -R "$TEMPLATE_DIR/.agents" "$TARGET/.agents"

echo "Installed Antigravity workflow kit into $TARGET"
echo "  - AGENTS.md"
echo "  - .agents/agents/ (coordinator, explorer, implementer, verifier)"
echo "  - .agents/skills/ (minimal-scope, verify-before-done, run-ci, tdd, code-review, implement, to-spec, writing-for-agents)"
echo "  - .agents/rules/"
echo "  - .agents/workflows/feature-cycle.md"
echo ""
echo "Next: edit AGENTS.md verification commands for your stack. See SETUP.md."
