# Tool names (Antigravity markdown agents)

Invalid tool names in agent `tools:` frontmatter can **hang** the subagent (Antigravity known issue).

This kit only lists names documented for markdown custom agents:

| Name | Used by |
|------|---------|
| `view_file` | all |
| `grep_search` | research / verify |
| `replace_file_content` | implementer only |
| `run_command` | coordinator, implementer, verifier |
| `manage_task` | coordinator, implementer |
| `invoke_subagent` | coordinator |

**Do not add** speculative names (`list_dir`, `list_directory`, `write_to_file`, `create_file`, `start_subagent`, etc.) unless you confirm them in your Antigravity build's docs — wrong names may hang.

If implementer cannot create a new planned file with `replace_file_content`, use `run_command` carefully or ask the user which write tool your build exposes.
