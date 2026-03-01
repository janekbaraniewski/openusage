import { useMemo, useState } from "react";
import AnimatedSection from "./ui/AnimatedSection";
import ClaudeCodeView from "./tui/views/ClaudeCodeView";
import CursorView from "./tui/views/CursorView";
import OpenRouterView from "./tui/views/OpenRouterView";
import CopilotView from "./tui/views/CopilotView";
import GeminiView from "./tui/views/GeminiView";
import CodexView from "./tui/views/CodexView";
import OpenCodeRuntime from "./OpenCodeRuntime";

const modules = [
  { id: "claude", label: "Claude Code", status: "OK" },
  { id: "cursor", label: "Cursor", status: "WARN" },
  { id: "openrouter", label: "OpenRouter", status: "OK" },
  { id: "copilot", label: "Copilot", status: "OK" },
  { id: "gemini", label: "Gemini CLI", status: "OK" },
  { id: "codex", label: "Codex CLI", status: "LIMIT" },
] as const;

type ModuleId = (typeof modules)[number]["id"];

function renderModule(id: ModuleId, bodyMinWidth: number) {
  if (id === "claude") return <ClaudeCodeView bodyMinWidth={bodyMinWidth} />;
  if (id === "cursor") return <CursorView bodyMinWidth={bodyMinWidth} />;
  if (id === "openrouter") return <OpenRouterView bodyMinWidth={bodyMinWidth} />;
  if (id === "copilot") return <CopilotView bodyMinWidth={bodyMinWidth} />;
  if (id === "gemini") return <GeminiView bodyMinWidth={bodyMinWidth} />;
  return <CodexView bodyMinWidth={bodyMinWidth} />;
}

export default function DashboardReplicaStudio() {
  const [active, setActive] = useState<ModuleId>("claude");

  const selected = useMemo(() => modules.find((m) => m.id === active) ?? modules[0], [active]);

  return (
    <>
      <section className="section section-replica" id="providers">
        <div className="container">
          <AnimatedSection className="section-head">
            <p className="section-label">Exact HTML Replica</p>
            <h2>Dashboard and widgets rebuilt 1:1 in HTML/CSS/JS.</h2>
            <p>
              Six provider modules are rendered as terminal widgets. No image artifacts in the widget
              layer.
            </p>
          </AnimatedSection>

          <div className="studio-shell">
            <div className="studio-head">
              <span>openusage dashboard / compact 6-provider layout</span>
              <span>widgets rendered in frontend</span>
            </div>

            <div className="studio-grid">
              {modules.map((module) => (
                <article key={module.id} className="studio-cell">
                  {renderModule(module.id, 430)}
                </article>
              ))}
            </div>
          </div>

          <div className="focus-shell">
            <div className="focus-rail" role="tablist" aria-label="Provider focus module">
              {modules.map((module) => (
                <button
                  key={module.id}
                  type="button"
                  role="tab"
                  className={module.id === selected.id ? "is-active" : ""}
                  aria-selected={module.id === selected.id}
                  onClick={() => setActive(module.id)}
                >
                  <span>{module.label}</span>
                  <small>{module.status}</small>
                </button>
              ))}
            </div>

            <div className="focus-panel">{renderModule(selected.id, 760)}</div>
          </div>
        </div>
      </section>

      <section className="section section-side-by-side" id="side-by-side">
        <div className="container">
          <AnimatedSection className="section-head">
            <p className="section-label">Side By Side Runtime</p>
            <h2>OpenUsage dashboard next to an OpenCode session.</h2>
            <p>
              This recreates the operating setup: monitor live costs in OpenUsage while coding in
              OpenCode.
            </p>
          </AnimatedSection>

          <div className="runtime-pair">
            <div className="runtime-col">{renderModule(selected.id, 560)}</div>
            <div className="runtime-col">
              <OpenCodeRuntime />
            </div>
          </div>
        </div>
      </section>
    </>
  );
}
