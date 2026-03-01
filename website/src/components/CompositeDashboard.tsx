import type { ReactElement } from "react";
import ClaudeCodeView from "./tui/views/ClaudeCodeView";
import CursorView from "./tui/views/CursorView";
import OpenRouterView from "./tui/views/OpenRouterView";
import CopilotView from "./tui/views/CopilotView";
import GeminiView from "./tui/views/GeminiView";
import CodexView from "./tui/views/CodexView";

export type ModuleId = "claude" | "cursor" | "openrouter" | "copilot" | "gemini" | "codex";

export interface ModuleSpec {
  id: ModuleId;
  title: string;
  width: number;
  height: number;
  status: "OK" | "WARN" | "LIMIT";
  render: (bodyMinWidth: number) => ReactElement;
}

export const moduleSpecs: ModuleSpec[] = [
  {
    id: "claude",
    title: "Claude Code",
    width: 1434,
    height: 2084,
    status: "OK",
    render: (bodyMinWidth) => <ClaudeCodeView bodyMinWidth={bodyMinWidth} />,
  },
  {
    id: "cursor",
    title: "Cursor",
    width: 1432,
    height: 2220,
    status: "WARN",
    render: (bodyMinWidth) => <CursorView bodyMinWidth={bodyMinWidth} />,
  },
  {
    id: "openrouter",
    title: "OpenRouter",
    width: 1440,
    height: 2092,
    status: "OK",
    render: (bodyMinWidth) => <OpenRouterView bodyMinWidth={bodyMinWidth} />,
  },
  {
    id: "copilot",
    title: "Copilot",
    width: 1432,
    height: 1786,
    status: "OK",
    render: (bodyMinWidth) => <CopilotView bodyMinWidth={bodyMinWidth} />,
  },
  {
    id: "gemini",
    title: "Gemini CLI",
    width: 1432,
    height: 1932,
    status: "OK",
    render: (bodyMinWidth) => <GeminiView bodyMinWidth={bodyMinWidth} />,
  },
  {
    id: "codex",
    title: "Codex CLI",
    width: 1442,
    height: 2188,
    status: "LIMIT",
    render: (bodyMinWidth) => <CodexView bodyMinWidth={bodyMinWidth} />,
  },
];

interface ScaledModuleProps {
  spec: ModuleSpec;
  scale: number;
}

export function ScaledModule({ spec, scale }: ScaledModuleProps) {
  const shellWidth = Math.round(spec.width * scale);
  const shellHeight = Math.round(spec.height * scale);

  return (
    <article className="replica-shell" style={{ width: shellWidth, height: shellHeight }}>
      <div
        className="replica-shell-inner"
        style={{
          width: spec.width,
          transform: `scale(${scale})`,
          transformOrigin: "top left",
        }}
      >
        {spec.render(spec.width - 20)}
      </div>
    </article>
  );
}

interface CompositeDashboardProps {
  scale: number;
}

export default function CompositeDashboard({ scale }: CompositeDashboardProps) {
  return (
    <div className="composite-grid" aria-label="OpenUsage 6-module dashboard replica">
      {moduleSpecs.map((spec) => (
        <div key={spec.id} className="composite-cell">
          <ScaledModule spec={spec} scale={scale} />
        </div>
      ))}
    </div>
  );
}
