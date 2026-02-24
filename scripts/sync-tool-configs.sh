#!/usr/bin/env bash
#
# sync-tool-configs.sh — Generate all AI tool config files from the canonical template.
#
# Source of truth:
#   docs/skills/tool-configs/template.md    (layout)
#   docs/skills/tool-configs/skills-table.md (skills table rows)
#
# Generated files:
#   .continuerules
#   .windsurfrules
#   .github/copilot-instructions.md
#   .aider/conventions.md
#   .opencode/skills/*/SKILL.md (skill stubs)
#   .claude/commands/*.md (command stubs)
#
# Usage:
#   ./scripts/sync-tool-configs.sh          # from repo root
#   make sync-tools                         # via Makefile

set -eo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TEMPLATE="$REPO_ROOT/docs/skills/tool-configs/template.md"
SKILLS_TABLE="$REPO_ROOT/docs/skills/tool-configs/skills-table.md"

if [[ ! -f "$TEMPLATE" ]]; then
  echo "Error: Template not found at $TEMPLATE" >&2
  exit 1
fi

if [[ ! -f "$SKILLS_TABLE" ]]; then
  echo "Error: Skills table not found at $SKILLS_TABLE" >&2
  exit 1
fi

# generate_config <title> <output_file>
generate_config() {
  local title="$1"
  local output="$2"

  mkdir -p "$(dirname "$output")"

  sed \
    -e "s|{{TOOL_TITLE}}|$title|g" \
    -e "/{{SKILLS_TABLE}}/{
      r $SKILLS_TABLE
      d
    }" \
    "$TEMPLATE" > "$output"

  echo "  Generated: $output"
}

# skill_description <skill-name>
# Returns a short description for each skill
skill_description() {
  case "$1" in
    add-new-provider)       echo "Add a new AI provider to the dashboard" ;;
    design-feature)         echo "Design a feature: quiz, explore codebase, write design doc with tasks" ;;
    develop-feature)        echo "Develop a feature end-to-end from design to pull request" ;;
    finalize-feature)       echo "Finalize a feature: create branch, commit, open PR" ;;
    implement-feature)      echo "Implement a feature from its design doc with tests" ;;
    iterate-feature)        echo "Iterate on a feature to fix issues and address feedback" ;;
    review-design)          echo "Review a design doc against the codebase" ;;
    validate-feature)       echo "Validate a feature implementation: build, tests, compliance, quality" ;;
    dev-workflow-improvements) echo "Audit and improve the development workflow, sync tool configs" ;;
    *)                      echo "Run the $1 skill" ;;
  esac
}

# title_case <hyphenated-string>
# Converts "design-feature" to "Design Feature"
title_case() {
  echo "$1" | sed 's/-/ /g' | awk '{for(i=1;i<=NF;i++) $i=toupper(substr($i,1,1)) substr($i,2)}1'
}

echo "Syncing tool configs from template..."
echo ""

# Generate each tool config
generate_config "Continue.dev Rules"          "$REPO_ROOT/.continuerules"
generate_config "Windsurf Rules"              "$REPO_ROOT/.windsurfrules"
generate_config "GitHub Copilot Instructions" "$REPO_ROOT/.github/copilot-instructions.md"
generate_config "Aider Conventions"           "$REPO_ROOT/.aider/conventions.md"

# --- OpenCode skill stubs ---
echo ""
echo "Syncing OpenCode skill stubs..."

SKILLS_DIR="$REPO_ROOT/docs/skills"
OPENCODE_DIR="$REPO_ROOT/.opencode/skills"

for skill_dir in "$SKILLS_DIR"/*/; do
  skill_name=$(basename "$skill_dir")

  # Skip directories without a SKILL.md
  if [[ ! -f "$skill_dir/SKILL.md" ]]; then
    continue
  fi

  desc=$(skill_description "$skill_name")
  pretty_name=$(title_case "$skill_name")
  target_dir="$OPENCODE_DIR/$skill_name"
  target_file="$target_dir/SKILL.md"

  mkdir -p "$target_dir"

  cat > "$target_file" <<EOF
# Skill: $pretty_name

> **Invocation**: $desc

Read and follow the full skill specification in \`docs/skills/$skill_name/SKILL.md\`.
EOF

  echo "  Generated: $target_file"
done

# --- Claude Code command stubs ---
echo ""
echo "Syncing Claude Code command stubs..."

CLAUDE_CMD_DIR="$REPO_ROOT/.claude/commands"
mkdir -p "$CLAUDE_CMD_DIR"

# claude_command_content <skill-name>
# Returns the full content for a Claude command stub
claude_command_content() {
  case "$1" in
    design-feature)
      cat <<'CMDEOF'
Design a new feature "$ARGUMENTS" for the OpenUsage TUI dashboard.

Read and follow the full skill specification in docs/skills/design-feature/SKILL.md.

Follow all phases in order:

1. **Phase 0 — Quiz**: Ask me all 8 questions from the skill doc before doing any design work. If I provided the feature name as "$ARGUMENTS", use that as the starting point but still confirm details. Research the codebase yourself if I don't know an answer.

2. **Phase 1 — Explore**: Read the subsystem map in docs/skills/design-feature/references/subsystem-map.md, then read the primary files for every affected subsystem. Read any overlapping design docs in docs/. Summarize what you learned that affects the design.

3. **Phase 2 — Design**: Write the design doc to docs/<FEATURE_NAME>_DESIGN.md following the template in docs/skills/design-feature/references/design-template.md. Keep it simple — no unnecessary abstractions.

4. **Phase 3 — Tasks**: Break the design into concrete, ordered implementation tasks with specific files and tests. Append to the design doc.

Complete the full checklist at the end of the skill doc before finishing.
CMDEOF
      ;;
    develop-feature)
      cat <<'CMDEOF'
Develop the feature "$ARGUMENTS" end-to-end — from design to pull request.

Read and follow the full skill specification in docs/skills/develop-feature/SKILL.md.

This skill orchestrates the full development lifecycle:

1. **Phase 0 — Intake**: Check for existing design doc. Ask: full lifecycle or specific phase?

2. **Phase 1 — Design** (`/design-feature`): Design the feature, produce design doc with tasks.

3. **Phase 2 — Review** (`/review-design`): Validate design against codebase, fix discrepancies.

4. **Phase 3 — Implement** (`/implement-feature`): Execute tasks with tests, parallel where possible.

5. **Phase 4 — Validate** (`/validate-feature`): Build, test, design compliance, code quality checks.

6. **Phase 5 — Iterate** (`/iterate-feature`): Fix issues from validation (loops until clean or user decides).

7. **Phase 6 — Finalize** (`/finalize-feature`): Create branch, commit, open PR.

8. **Phase 7 — Summary**: Report full lifecycle results.

Each phase pauses for user confirmation before proceeding to the next.
CMDEOF
      ;;
    implement-feature)
      cat <<'CMDEOF'
Implement the feature "$ARGUMENTS" from its design doc.

Read and follow the full skill specification in docs/skills/implement-feature/SKILL.md.

Follow all phases in order:

1. **Phase 0 — Load**: Read the design doc, extract tasks and scope.
2. **Phase 1 — Codebase Analysis**: Read affected files, note patterns.
3. **Phase 1.5 — Pre-Implementation Quiz**: Surface ambiguities.
4. **Phase 2 — Execution Plan**: Present tasks with approaches and risks.
5. **Phase 3 — Implement**: Execute tasks in dependency order with tests.
6. **Phase 4 — Integration Check**: Build, test, verify.
7. **Phase 5 — Summary**: Report changes and status.
CMDEOF
      ;;
    review-design)
      cat <<'CMDEOF'
Review the design doc for "$ARGUMENTS" against the current codebase.

Read and follow the full skill specification in docs/skills/review-design/SKILL.md.

Follow all phases:

1. **Phase 0 — Load**: Find and read the design doc.
2. **Phase 1 — Audit**: Read primary files for each subsystem, build discrepancy list.
3. **Phase 2 — Quiz Loop**: Present issues, apply resolutions, re-scan until clean.
4. **Phase 3 — Verify**: Confirm tasks reference valid files and types.
CMDEOF
      ;;
    validate-feature)
      cat <<'CMDEOF'
Validate the feature "$ARGUMENTS" implementation.

Read and follow the full skill specification in docs/skills/validate-feature/SKILL.md.

Follow all phases:

1. **Phase 0 — Load**: Find design doc, extract tasks, get changed files.
2. **Phase 1 — Build**: `make build`, `make vet`, `make fmt`, `make lint`.
3. **Phase 2 — Tests**: Run tests for changed packages.
4. **Phase 3 — Compliance**: Cross-reference design tasks vs actual changes.
5. **Phase 4 — Quality**: Scan for debug artifacts, unused code, secrets.
6. **Phase 5 — Smoke Test**: Final build and combined tests.
7. **Phase 6 — Report**: Verdict: READY FOR REVIEW or NEEDS ITERATION.
CMDEOF
      ;;
    iterate-feature)
      cat <<'CMDEOF'
Iterate on the feature "$ARGUMENTS" to fix issues and address feedback.

Read and follow the full skill specification in docs/skills/iterate-feature/SKILL.md.

Follow all phases:

1. **Phase 0 — Load**: Find design doc, gather feedback.
2. **Phase 1 — Triage**: Categorize issues by priority.
3. **Phase 2 — Plan**: Identify files and approach for each fix.
4. **Phase 3 — Execute**: Fix, test, verify each issue.
5. **Phase 4 — Re-validate**: Build, test, check compliance.
6. **Phase 5 — Summary**: Report fixes and verdict.
CMDEOF
      ;;
    finalize-feature)
      cat <<'CMDEOF'
Finalize the feature "$ARGUMENTS" — create branch, commit, and open PR.

Read and follow the full skill specification in docs/skills/finalize-feature/SKILL.md.

Follow all phases:

1. **Phase 0 — Pre-flight**: Build, vet, tests pass. Check for secrets.
2. **Phase 1 — Branch**: Create feature branch.
3. **Phase 2 — Commit**: Draft message, show to user, stage specific files, commit.
4. **Phase 3 — PR**: Push and create PR via `gh pr create`.
5. **Phase 4 — Checklist**: Report branch, commit, PR URL.
CMDEOF
      ;;
    add-new-provider)
      cat <<'CMDEOF'
Add a new AI provider "$ARGUMENTS" to the OpenUsage TUI dashboard.

Read and follow the full skill specification in docs/skills/add-new-provider.md.

Follow all phases:

1. **Phase 0 — Quiz**: Ask all 10 provider questions.
2. **Phase 1 — Research**: Study provider API docs.
3. **Phase 2 — Create Package**: Implement provider in `internal/providers/<id>/`.
4. **Phase 3 — Dashboard Widget**: Create tile with gauges and compact rows.
5. **Phase 4 — Register**: Add to registry.go, detect.go, example_settings.json.
6. **Phase 5 — Tests**: Minimum 3 tests using httptest.NewServer.
7. **Phase 6 — Verify**: `go build`, `go test`, `make vet`.
CMDEOF
      ;;
    dev-workflow-improvements)
      cat <<'CMDEOF'
Audit and improve the development workflow for OpenUsage.

Read and follow the full skill specification in docs/skills/dev-workflow-improvements/SKILL.md.

This skill ensures the development flow is complete, consistent, and propagated to all AI tools.

Follow all phases:

1. **Phase 0 — Audit**: Run `make sync-tools`, check for drift. Validate all skills are registered in skills-table.md, have Claude commands, OpenCode stubs. Check for broken references.

2. **Phase 1 — Fix**: Fix any issues found: sync drift, missing registrations, broken references, CLAUDE.md mismatches.

3. **Phase 2 — Improve**: If improvements requested, quiz the user about what needs changing. Add/update skills, onboard new tools, fix workflow gaps. Run sync after each change.

4. **Phase 3 — Verify**: Run sync (should be clean), build, test, show git diff for review.
CMDEOF
      ;;
    *)
      # Generic fallback for skills without custom Claude command content
      local desc
      desc=$(skill_description "$1")
      cat <<CMDEOF
$desc

Read and follow the full skill specification in docs/skills/$1/SKILL.md.
CMDEOF
      ;;
  esac
}

for skill_dir in "$SKILLS_DIR"/*/; do
  skill_name=$(basename "$skill_dir")

  # Skip directories without a SKILL.md
  if [[ ! -f "$skill_dir/SKILL.md" ]]; then
    continue
  fi

  target_file="$CLAUDE_CMD_DIR/$skill_name.md"
  claude_command_content "$skill_name" > "$target_file"
  echo "  Generated: $target_file"
done

echo ""
echo "Done. All tool configs are in sync."
echo ""
echo "Files generated:"
echo "  .continuerules"
echo "  .windsurfrules"
echo "  .github/copilot-instructions.md"
echo "  .aider/conventions.md"
echo "  .opencode/skills/*/SKILL.md"
echo "  .claude/commands/*.md"
