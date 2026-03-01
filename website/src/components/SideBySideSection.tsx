import AnimatedSection from "./ui/AnimatedSection";

const runTape = [
  "$ openusage telemetry daemon",
  "... collecting provider events",
  "$ opencode",
  "... coding session in progress",
  "$ openusage",
  "> compare burn, quotas, and model mix while coding",
];

export default function SideBySideSection() {
  return (
    <section className="section section-side-by-side" id="side-by-side">
      <div className="container">
        <AnimatedSection className="section-head">
          <p className="section-label">OpenUsage + OpenCode</p>
          <h2>How it looks when both runtimes are running side-by-side.</h2>
          <p>
            This section uses the real side-by-side capture so visitors can understand the operational
            setup immediately.
          </p>
        </AnimatedSection>

        <div className="sxs-shell">
          <div className="sxs-head">
            <span>live workflow capture</span>
            <span>openusage + opencode</span>
          </div>

          <div className="sxs-image-wrap">
            <img
              src={`${import.meta.env.BASE_URL}assets/sidebyside.png`}
              alt="OpenUsage dashboard and OpenCode running side by side"
            />
          </div>

          <div className="sxs-console" aria-label="Runtime command tape">
            {runTape.map((line) => (
              <p key={line}>{line}</p>
            ))}
          </div>
        </div>
      </div>
    </section>
  );
}
