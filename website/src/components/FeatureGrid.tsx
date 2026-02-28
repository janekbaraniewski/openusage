import AnimatedSection from "./ui/AnimatedSection";

const features = [
  {
    title: "Autodetects local tooling",
    desc: "Finds installed coding agents and key env vars automatically, so teams do not maintain brittle manual config.",
    accent: "text-teal",
  },
  {
    title: "Tracks quota and spend in one view",
    desc: "Budgets, rate limits, token burn, and remaining credits are normalized into one dashboard with consistent semantics.",
    accent: "text-amber",
  },
  {
    title: "Works with daemon + hooks",
    desc: "Run an always-on telemetry daemon and enrich it with Codex / Claude Code / OpenCode hook events for deeper fidelity.",
    accent: "text-mint",
  },
  {
    title: "Model-level diagnostics",
    desc: "Inspect cost by model, client, and workflow path to understand where expensive usage patterns begin.",
    accent: "text-coral",
  },
  {
    title: "Terminal-native by design",
    desc: "Fast Bubble Tea interface built for keyboard-first workflows where engineers already operate.",
    accent: "text-teal",
  },
  {
    title: "Private, local-first runtime",
    desc: "Settings and telemetry stay on your machine using local config and SQLite storage, with no hosted dependency required.",
    accent: "text-amber",
  },
];

export default function FeatureGrid() {
  return (
    <section id="features" className="py-18 sm:py-22">
      <div className="mx-auto max-w-6xl px-6">
        <AnimatedSection>
          <p className="mb-3 font-mono text-[11px] uppercase tracking-[0.17em] text-faint">
            Why teams switch
          </p>
          <h2 className="max-w-3xl text-3xl font-semibold tracking-tight text-text sm:text-4xl">
            Built for the way modern AI engineering stacks actually behave.
          </h2>
          <p className="mt-4 max-w-2xl text-base leading-relaxed text-muted">
            Inspired by the clarity in OpenCode and Factory-style product pages,
            but focused on operator-grade telemetry instead of agent UX.
          </p>
        </AnimatedSection>

        <div className="mt-10 grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {features.map((feature, idx) => (
            <AnimatedSection key={feature.title} delay={idx * 0.05}>
              <article className="h-full rounded-2xl border border-line-soft bg-surface/85 p-5 transition-all duration-200 hover:-translate-y-1 hover:border-line hover:bg-panel/70">
                <p className={`mb-3 font-mono text-[11px] uppercase tracking-[0.15em] ${feature.accent}`}>
                  0{idx + 1}
                </p>
                <h3 className="text-lg font-semibold text-text">{feature.title}</h3>
                <p className="mt-2 text-sm leading-relaxed text-muted">{feature.desc}</p>
              </article>
            </AnimatedSection>
          ))}
        </div>
      </div>
    </section>
  );
}
