import AnimatedSection from "./ui/AnimatedSection";

const outcomes = [
  {
    metric: "< 5 min",
    label: "From install to first cost snapshot",
  },
  {
    metric: "16 providers",
    label: "Unified by one runtime and one dashboard",
  },
  {
    metric: "0 cloud dependency",
    label: "Local-first telemetry with full operator control",
  },
  {
    metric: "MIT licensed",
    label: "Open source and extensible by design",
  },
];

const artifacts = [
  {
    title: "Quota horizon",
    value: "73%",
    description: "Project how long current burn-rates can sustain active development.",
  },
  {
    title: "Model drift",
    value: "+24%",
    description: "Catch expensive model-default changes before they become policy.",
  },
  {
    title: "Idle leakage",
    value: "$312/mo",
    description: "Identify background spend from unattended agents and stale jobs.",
  },
];

export default function ProofSection() {
  return (
    <section className="section section-proof" id="proof">
      <div className="container">
        <AnimatedSection className="section-head">
          <p className="section-label">Proof Layer</p>
          <h2>A product story built on operator outcomes, not marketing claims.</h2>
        </AnimatedSection>

        <div className="outcome-grid">
          {outcomes.map((item, index) => (
            <AnimatedSection key={item.metric + item.label} delay={index * 0.06}>
              <article className="outcome-card">
                <strong>{item.metric}</strong>
                <p>{item.label}</p>
              </article>
            </AnimatedSection>
          ))}
        </div>

        <div className="artifact-grid">
          {artifacts.map((item, index) => (
            <AnimatedSection key={item.title} delay={index * 0.08}>
              <article className="artifact-card">
                <p>{item.title}</p>
                <strong>{item.value}</strong>
                <span>{item.description}</span>
              </article>
            </AnimatedSection>
          ))}
        </div>
      </div>
    </section>
  );
}
