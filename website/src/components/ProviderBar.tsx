import { providers } from "../data/providers";
import ProviderIcon from "./ui/ProviderIcon";
import AnimatedSection from "./ui/AnimatedSection";

export default function ProviderBar() {
  const agents = providers.filter(
    (provider) => provider.category === "agent" || provider.category === "local",
  );
  const apis = providers.filter((provider) => provider.category === "api");

  return (
    <section id="providers" className="py-18 sm:py-22">
      <div className="mx-auto max-w-6xl px-6">
        <AnimatedSection>
          <p className="mb-3 font-mono text-[11px] uppercase tracking-[0.17em] text-faint">
            Integrations
          </p>
          <h2 className="max-w-3xl text-3xl font-semibold tracking-tight text-text sm:text-4xl">
            One dashboard across coding agents and raw API platforms.
          </h2>
        </AnimatedSection>

        <div className="mt-8 grid gap-5 lg:grid-cols-2">
          <AnimatedSection delay={0.04}>
            <div className="rounded-2xl border border-line-soft bg-surface/85 p-5">
              <p className="mb-4 font-mono text-[11px] uppercase tracking-[0.13em] text-teal">
                Agent tools
              </p>
              <div className="grid gap-2 sm:grid-cols-2">
                {agents.map((provider) => (
                  <div
                    key={provider.slug}
                    className="rounded-xl border border-line-soft bg-panel/50 px-3 py-2"
                  >
                    <div className="flex items-center gap-2">
                      <ProviderIcon slug={provider.icon} name={provider.name} size={18} />
                      <p className="text-sm font-medium text-text">{provider.name}</p>
                    </div>
                    <p className="mt-1 font-mono text-[10px] uppercase tracking-[0.08em] text-faint">
                      {provider.detection}
                    </p>
                  </div>
                ))}
              </div>
            </div>
          </AnimatedSection>

          <AnimatedSection delay={0.08}>
            <div className="rounded-2xl border border-line-soft bg-surface/85 p-5">
              <p className="mb-4 font-mono text-[11px] uppercase tracking-[0.13em] text-amber">
                API providers
              </p>
              <div className="grid gap-2 sm:grid-cols-2">
                {apis.map((provider) => (
                  <div
                    key={provider.slug}
                    className="rounded-xl border border-line-soft bg-panel/50 px-3 py-2"
                  >
                    <div className="flex items-center gap-2">
                      <ProviderIcon slug={provider.icon} name={provider.name} size={18} />
                      <p className="text-sm font-medium text-text">{provider.name}</p>
                    </div>
                    <p className="mt-1 font-mono text-[10px] uppercase tracking-[0.08em] text-faint">
                      {provider.detection}
                    </p>
                  </div>
                ))}
              </div>
            </div>
          </AnimatedSection>
        </div>
      </div>
    </section>
  );
}
