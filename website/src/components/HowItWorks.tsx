import AnimatedSection from "./ui/AnimatedSection";
import CodeBlock from "./ui/CodeBlock";

const steps = [
  {
    title: "Install and open dashboard",
    desc: "Start with one command. OpenUsage detects what is already on your workstation.",
    code: "openusage",
  },
  {
    title: "Enable telemetry daemon",
    desc: "Collect usage snapshots continuously while you work across IDE, CLI, and API flows.",
    code: "openusage telemetry daemon install",
  },
  {
    title: "Attach optional hooks",
    desc: "Enrich usage data from Codex, Claude Code, and OpenCode events for better attribution.",
    code: "openusage integrations install codex",
  },
];

export default function HowItWorks() {
  return (
    <section id="workflow" className="py-18 sm:py-22">
      <div className="mx-auto max-w-6xl px-6">
        <AnimatedSection>
          <p className="mb-3 font-mono text-[11px] uppercase tracking-[0.17em] text-faint">
            Workflow
          </p>
          <h2 className="max-w-3xl text-3xl font-semibold tracking-tight text-text sm:text-4xl">
            Three steps from blank terminal to full visibility.
          </h2>
        </AnimatedSection>

        <div className="mt-8 grid gap-4 lg:grid-cols-3">
          {steps.map((step, idx) => (
            <AnimatedSection key={step.title} delay={idx * 0.07}>
              <article className="h-full rounded-2xl border border-line-soft bg-surface/85 p-5">
                <p className="font-mono text-[11px] uppercase tracking-[0.15em] text-teal">
                  Step {idx + 1}
                </p>
                <h3 className="mt-2 text-lg font-semibold text-text">{step.title}</h3>
                <p className="mt-2 text-sm leading-relaxed text-muted">{step.desc}</p>
                <div className="mt-4">
                  <CodeBlock code={step.code} />
                </div>
              </article>
            </AnimatedSection>
          ))}
        </div>
      </div>
    </section>
  );
}
