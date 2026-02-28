import AnimatedSection from "./ui/AnimatedSection";

export default function OpenSource() {
  return (
    <section className="py-18 sm:py-20">
      <div className="mx-auto max-w-6xl px-6">
        <AnimatedSection>
          <div className="grid gap-6 rounded-2xl border border-line-soft bg-surface/80 p-6 sm:grid-cols-[1.15fr_0.85fr] sm:p-7">
            <div>
              <p className="mb-3 font-mono text-[11px] uppercase tracking-[0.17em] text-faint">
                Open source
              </p>
              <h2 className="text-2xl font-semibold tracking-tight text-text sm:text-3xl">
                MIT licensed. Built in Go. Optimized for teams that want local control.
              </h2>
              <p className="mt-3 max-w-2xl text-sm leading-relaxed text-muted sm:text-base">
                OpenUsage ships as a terminal-first binary with a telemetry daemon,
                structured provider adapters, and practical extension points for
                integrations.
              </p>
            </div>

            <div className="grid gap-3 text-sm">
              <a
                href="https://github.com/janekbaraniewski/openusage"
                target="_blank"
                rel="noopener noreferrer"
                className="rounded-xl border border-line-soft bg-panel/40 px-4 py-3 font-medium text-text transition-colors hover:border-teal hover:text-teal"
              >
                Repository
              </a>
              <a
                href="https://github.com/janekbaraniewski/openusage/releases"
                target="_blank"
                rel="noopener noreferrer"
                className="rounded-xl border border-line-soft bg-panel/40 px-4 py-3 font-medium text-text transition-colors hover:border-teal hover:text-teal"
              >
                Releases
              </a>
              <a
                href="https://github.com/janekbaraniewski/openusage/issues"
                target="_blank"
                rel="noopener noreferrer"
                className="rounded-xl border border-line-soft bg-panel/40 px-4 py-3 font-medium text-text transition-colors hover:border-teal hover:text-teal"
              >
                Issues and roadmap
              </a>
            </div>
          </div>
        </AnimatedSection>
      </div>
    </section>
  );
}
