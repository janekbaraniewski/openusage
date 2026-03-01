import AnimatedSection from "./ui/AnimatedSection";

const pipeline = [
  {
    label: "Collect",
    command: "openusage telemetry daemon",
    detail: "Runs a local runtime that continuously ingests provider snapshots + hook events.",
  },
  {
    label: "Normalize",
    command: "openusage telemetry daemon status",
    detail: "Unifies costs, quotas, and model usage under one read model for apples-to-apples decisions.",
  },
  {
    label: "Act",
    command: "openusage",
    detail: "Open a terminal dashboard that reveals who is burning tokens and where intervention is needed.",
  },
];

const providers = ["Codex", "Cursor", "Claude Code", "Copilot", "Gemini", "OpenRouter"];

export default function ControlSurface() {
  return (
    <section className="section section-control" id="control">
      <div className="container control-layout">
        <AnimatedSection className="section-head">
          <p className="section-label">Control Surface</p>
          <h2>Three precise loops: collect, normalize, and intervene in real time.</h2>
          <p>
            OpenUsage is not another screenshot dashboard. It is an operational loop tuned for teams
            running serious AI workloads.
          </p>
        </AnimatedSection>

        <div className="pipeline-grid">
          {pipeline.map((step, index) => (
            <AnimatedSection key={step.label} delay={index * 0.08}>
              <article className="pipeline-card">
                <p className="pipeline-step">{step.label}</p>
                <code>{step.command}</code>
                <p>{step.detail}</p>
              </article>
            </AnimatedSection>
          ))}
        </div>

        <AnimatedSection delay={0.2}>
          <div className="provider-wave" aria-label="Supported providers">
            {providers.map((provider) => (
              <span key={provider}>{provider}</span>
            ))}
          </div>
        </AnimatedSection>
      </div>
    </section>
  );
}
