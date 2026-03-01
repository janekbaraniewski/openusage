import AnimatedSection from "./ui/AnimatedSection";

const blindSpots = [
  {
    title: "Usage hides in 6 different places",
    text: "Provider APIs, CLI tools, editor extensions, and local hooks all report differently.",
  },
  {
    title: "Budgets fail silently",
    text: "Teams notice burn-rate spikes after invoices land instead of during development.",
  },
  {
    title: "Model choices stay unaccountable",
    text: "Without normalized telemetry, expensive defaults become the accidental standard.",
  },
];

export default function RealitySection() {
  return (
    <section className="section section-reality" id="signal">
      <div className="container">
        <AnimatedSection>
          <div className="section-head">
            <p className="section-label">Reality Check</p>
            <h2>Most teams are fast at shipping agents and blind on spend.</h2>
          </div>
        </AnimatedSection>

        <div className="reality-grid">
          {blindSpots.map((item, index) => (
            <AnimatedSection key={item.title} delay={index * 0.08}>
              <article className="reality-card">
                <span>{`0${index + 1}`}</span>
                <h3>{item.title}</h3>
                <p>{item.text}</p>
              </article>
            </AnimatedSection>
          ))}
        </div>
      </div>
    </section>
  );
}
