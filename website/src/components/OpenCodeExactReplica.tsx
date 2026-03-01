const timeline = [
  "session#91  branch=codex/terminal-replica",
  "task> recreate dashboard widgets in pure html/css/js",
  "tool> rg --files",
  "tool> apply_patch website/src/components/tui/...",
  "tool> npm run build",
  "status> waiting for follow-up",
];

const editorLines = [
  "import { Runtime } from \"opencode\";",
  "",
  "const rt = new Runtime({",
  "  mode: \"terminal\",",
  "  tools: [\"shell\", \"patch\", \"tests\"],",
  "  objective: \"exact screenshot parity\",",
  "});",
  "",
  "await rt.execute();",
];

const stats = [
  ["context", "128k"],
  ["turns", "214"],
  ["tokens in", "4.8M"],
  ["tokens out", "1.2M"],
  ["tools", "shell/patch/git"],
];

export default function OpenCodeExactReplica() {
  return (
    <div className="opencode-replica" aria-label="OpenCode runtime replica">
      <div className="opencode-replica-head">
        <span>opencode</span>
        <span>interactive runtime</span>
      </div>

      <div className="opencode-replica-body">
        <section className="opencode-block">
          <p className="opencode-block-title">session.log</p>
          {timeline.map((line) => (
            <p key={line}>{line}</p>
          ))}
        </section>

        <section className="opencode-block">
          <p className="opencode-block-title">worker.ts</p>
          <pre>{editorLines.join("\n")}</pre>
        </section>

        <section className="opencode-block">
          <p className="opencode-block-title">runtime.stats</p>
          {stats.map(([k, v]) => (
            <p key={k}>
              <span>{k}</span>
              <strong>{v}</strong>
            </p>
          ))}
        </section>
      </div>

      <div className="opencode-replica-foot">
        <span>tools: shell · patch · git · tests</span>
        <span>state: running</span>
      </div>
    </div>
  );
}
