const activity = [
  "session: ui-redesign / branch codex/terminal-replica",
  "agent: opencode assistant running",
  "tool exec: rg --files",
  "tool exec: npm run build",
  "patch: updated website/src/index.css",
  "status: waiting for next instruction",
];

const code = [
  "import { createWorkflow } from \"opencode/runtime\";",
  "",
  "const workflow = createWorkflow({",
  "  objective: \"ship 1:1 terminal replica\",",
  "  tools: [\"shell\", \"patch\", \"test\"],",
  "});",
  "",
  "await workflow.run();",
];

export default function OpenCodeRuntime() {
  return (
    <div className="opencode-shell" aria-label="OpenCode runtime preview">
      <div className="opencode-head">
        <span>opencode runtime</span>
        <span>live session</span>
      </div>

      <div className="opencode-body">
        <div className="opencode-pane">
          <p className="opencode-pane-title">activity.log</p>
          {activity.map((line) => (
            <p key={line}>{line}</p>
          ))}
        </div>

        <div className="opencode-pane">
          <p className="opencode-pane-title">worker.ts</p>
          <pre>{code.join("\n")}</pre>
        </div>
      </div>

      <div className="opencode-foot">
        <span>tools: shell · patch · git · tests</span>
        <span>state: running</span>
      </div>
    </div>
  );
}
