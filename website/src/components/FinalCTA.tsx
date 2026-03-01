import AnimatedSection from "./ui/AnimatedSection";

const repoUrl = "https://github.com/janekbaraniewski/openusage";
const docsUrl = "https://github.com/janekbaraniewski/openusage#readme";

export default function FinalCTA() {
  return (
    <section className="section section-final">
      <div className="container">
        <AnimatedSection>
          <div className="final-card">
            <p className="section-label">Build Better Agent Ops</p>
            <h2>Ship ambitious agent systems without surrendering financial control.</h2>
            <p>
              OpenUsage gives your team a command center for AI spend, quota pressure, and model-level
              behavior before surprises reach finance.
            </p>
            <div className="hero-actions">
              <a href="#install" className="button button-solid">
                Install OpenUsage
              </a>
              <a href={docsUrl} target="_blank" rel="noopener noreferrer" className="button button-ghost">
                Read docs
              </a>
              <a href={repoUrl} target="_blank" rel="noopener noreferrer" className="button button-ghost">
                Star on GitHub
              </a>
            </div>
          </div>
        </AnimatedSection>
      </div>
    </section>
  );
}
