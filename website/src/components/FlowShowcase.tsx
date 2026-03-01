import { useEffect, useState } from "react";
import type { ReactElement } from "react";
import { AnimatePresence, motion, useReducedMotion } from "framer-motion";
import AnimatedSection from "./ui/AnimatedSection";
import CursorView from "./tui/views/CursorView";
import ClaudeCodeView from "./tui/views/ClaudeCodeView";
import OpenRouterView from "./tui/views/OpenRouterView";
import CopilotView from "./tui/views/CopilotView";

type FlowSpec = {
  id: string;
  label: string;
  summary: string;
  operator: string;
  commandTape: string[];
  view: "cursor" | "claude" | "openrouter" | "copilot";
};

const flows: FlowSpec[] = [
  {
    id: "spike",
    label: "Cost Spike Triage",
    summary: "Catch a sudden burn-rate jump and isolate which tool chain caused it.",
    operator: "Platform engineer",
    commandTape: [
      "$ openusage telemetry daemon status",
      "! burn alert: codex-cli +31% in last 25m",
      "$ openusage",
      "> filter provider=codex-cli window=1h",
      "> identify top model + top workflow",
      "✓ action: switch default model to gpt-5-mini",
    ],
    view: "cursor",
  },
  {
    id: "quota",
    label: "Quota Rescue",
    summary: "Detect imminent quota exhaustion before teams get blocked mid-deploy.",
    operator: "Infra lead",
    commandTape: [
      "$ openusage telemetry daemon",
      "... ingesting provider quotas every 60s",
      "! quota pressure: claude-code 89%",
      "$ openusage",
      "> drill into 5h + 7d windows",
      "✓ action: move long tasks to lower-cost model",
    ],
    view: "claude",
  },
  {
    id: "multi",
    label: "Multi-Provider Mix",
    summary: "Compare cost efficiency across providers and reroute workloads intentionally.",
    operator: "Staff engineer",
    commandTape: [
      "$ openusage integrations install codex",
      "$ openusage telemetry daemon status",
      "> openrouter usage rising; credits 78.5%",
      "> compare quality-per-dollar across providers",
      "✓ action: rebalance routing weights",
      "✓ result: projected spend down 18%",
    ],
    view: "openrouter",
  },
  {
    id: "govern",
    label: "Team Governance",
    summary: "Track seat-level behavior and keep team budgets healthy without slowing builders.",
    operator: "Engineering manager",
    commandTape: [
      "$ openusage",
      "> group by team + client",
      "> inspect tool call volume and premium model share",
      "! completion quota trend crossing threshold",
      "✓ action: update policy + notify team leads",
      "✓ outcome: stable weekly spend envelope",
    ],
    view: "copilot",
  },
];

function renderView(view: FlowSpec["view"]): ReactElement {
  if (view === "cursor") return <CursorView />;
  if (view === "claude") return <ClaudeCodeView />;
  if (view === "openrouter") return <OpenRouterView />;
  return <CopilotView />;
}

export default function FlowShowcase() {
  const reduced = useReducedMotion();
  const [active, setActive] = useState(0);

  useEffect(() => {
    if (reduced) {
      return undefined;
    }

    const timer = window.setInterval(() => {
      setActive((prev) => (prev + 1) % flows.length);
    }, 7600);

    return () => window.clearInterval(timer);
  }, [reduced]);

  const selected = flows[active];

  return (
    <section className="section section-flows" id="flows">
      <div className="container">
        <AnimatedSection className="section-head">
          <p className="section-label">Flow Simulator</p>
          <h2>Walk through live operator flows, not static marketing screenshots.</h2>
          <p>
            Each flow mirrors how teams actually use OpenUsage during incidents, planning, and daily
            governance.
          </p>
        </AnimatedSection>

        <div className="flow-shell">
          <div className="flow-rail" role="tablist" aria-label="OpenUsage flows">
            {flows.map((flow, index) => (
              <button
                key={flow.id}
                role="tab"
                aria-selected={index === active}
                className={index === active ? "is-active" : ""}
                onClick={() => setActive(index)}
              >
                {flow.label}
              </button>
            ))}
          </div>

          <div className="flow-body">
            <div className="flow-meta">
              <p className="flow-operator">{selected.operator}</p>
              <h3>{selected.label}</h3>
              <p>{selected.summary}</p>

              <div className="flow-console" aria-live="polite">
                {selected.commandTape.map((line) => (
                  <p key={line}>{line}</p>
                ))}
              </div>
            </div>

            <div className="flow-preview">
              <AnimatePresence mode="wait">
                <motion.div
                  key={selected.id}
                  initial={reduced ? false : { opacity: 0, y: 12 }}
                  animate={{ opacity: 1, y: 0 }}
                  exit={reduced ? undefined : { opacity: 0, y: -8 }}
                  transition={{ duration: 0.26, ease: "easeOut" }}
                >
                  {renderView(selected.view)}
                </motion.div>
              </AnimatePresence>
            </div>
          </div>
        </div>
      </div>
    </section>
  );
}
