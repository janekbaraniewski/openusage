# Skill: Finalize Feature

Create a branch, commit changes, and open a pull request for a completed feature.

## When to use

After `/validate-feature` (or `/iterate-feature`) reports READY FOR REVIEW. This is the last step before code review.

## Prerequisite

- Implementation complete and validated (clean build, passing tests)
- A design doc in `docs/*_DESIGN.md` (used for PR description)
- Uncommitted changes in working tree

## Phases

### Phase 0 — Pre-flight Checks

Before any git operations, verify the implementation is ready:

1. `make build` — must pass
2. `make vet` — must pass
3. `go test <changed packages> -count=1 -race` — must pass
4. Check `git status` — confirm there are changes to commit
5. Scan staged files for:
   - `.env` files or credentials — BLOCK if found
   - Debug prints (`fmt.Println` not in test files) — WARN
   - Large binary files — WARN
6. If any BLOCK issues: stop and report. Do not proceed.
7. If any WARN issues: report and ask user to confirm proceeding.

### Phase 1 — Branch

1. Ask user for the branch name. Suggest format: `feat/<short-desc>` or `<linear-id>/<short-desc>`.
   - If user provides a Linear ID, use `<linear-id>/<short-desc>` format
   - If no Linear ID, use `feat/<short-desc>`
   - Convert to lowercase, hyphens for spaces
2. Check if already on a feature branch (not `main`):
   - If yes: ask "You're on branch `<name>`. Use this branch or create a new one?"
   - If no: create and checkout the new branch from current HEAD
3. Confirm branch name with user before creating.

### Phase 2 — Commit

1. Run `git diff --stat` and `git status` to show what will be committed.
2. Present the list of changed files, grouped by type:
   ```
   ## Files to commit

   ### New files
   - internal/core/time_window.go

   ### Modified files
   - internal/config/config.go
   - internal/tui/model.go

   ### Test files
   - internal/core/time_window_test.go
   ```
3. Draft a commit message:
   - Use conventional commit format: `feat:`, `fix:`, `refactor:`, `test:`, `docs:`
   - All lowercase subject line
   - Body: summarize what changed and why (2-5 bullet points)
   - Reference design doc
   - If Linear ID available: include `Closes <LINEAR-ID>` in body
   - Always append `Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>`
4. Present the commit message to user. Ask: "Commit with this message, or edit?"
5. Stage files with `git add <specific files>` — never use `git add -A` or `git add .`
6. Exclude from staging:
   - `.env`, credentials, secrets
   - Binary files not in `bin/`
   - Temporary files, editor backups
   - Files not related to the feature
7. Create the commit.

### Phase 3 — Push & Pull Request

1. Push branch to remote: `git push -u origin <branch-name>`
2. Draft PR using information from:
   - Design doc (problem statement, goals)
   - Implementation changes (from git diff against main)
   - Commit messages
3. PR format:
   ```
   Title: <short, under 70 chars, matches conventional commit style>

   Body:
   ## Summary
   <1-3 bullet points from design doc problem statement + solution>

   ## Changes
   <grouped list of what changed, by subsystem>

   ## Design doc
   <link or path to design doc>

   ## Test plan
   - [ ] Unit tests pass for changed packages
   - [ ] Build compiles cleanly
   - [ ] <feature-specific test steps>

   🤖 Generated with [Claude Code](https://claude.com/claude-code)
   ```
4. Create PR: `gh pr create --title "..." --body "..."`
5. If Linear ID provided, the PR title or body should reference it for auto-linking.
6. Report the PR URL to user.

### Phase 4 — Final Checklist

```
## Finalization Complete

- [x] Pre-flight checks passed
- [x] Branch: <branch-name>
- [x] Commit: <short hash> <subject>
- [x] PR: <url>

### Next steps
- Review PR
- Address any CI failures
- If changes requested, run `/iterate-feature <name>` then amend/push
```

## Rules

1. NEVER force push — always regular push. If push fails due to remote changes, report and ask user.
2. NEVER commit secrets, credentials, or `.env` files — block and report.
3. NEVER use `git add -A` or `git add .` — always stage specific files.
4. NEVER create a commit without showing the message to the user first.
5. NEVER push to main directly — always use a feature branch.
6. Always use conventional commit format (lowercase, no period at end of subject).
7. Always include Co-Authored-By trailer.
8. If pre-flight checks fail, stop immediately — do not try to fix issues (that's `/iterate-feature`'s job).
9. If user is already on a feature branch with existing commits, ask before adding more commits.
10. PR description should be useful for reviewers — include context, not just a file list.

## Checklist

Before marking finalization complete:
- [ ] Pre-flight checks pass (build, vet, tests)
- [ ] No secrets or credentials in staged files
- [ ] Branch name follows convention
- [ ] Commit message follows conventional commit format
- [ ] Commit message reviewed by user
- [ ] Changes pushed to remote
- [ ] PR created with summary, changes, and test plan
- [ ] PR URL reported to user
