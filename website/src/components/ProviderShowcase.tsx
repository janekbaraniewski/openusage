import { useState } from "react";
import { AnimatePresence, motion, useReducedMotion } from "framer-motion";
import AnimatedSection from "./ui/AnimatedSection";
import { providerScreenshots } from "../data/providers";

export default function ProviderShowcase() {
  const [active, setActive] = useState(0);
  const reduced = useReducedMotion();
  const item = providerScreenshots[active];

  return (
    <section id="showcase" className="py-16 sm:py-20">
      <div className="mx-auto max-w-6xl px-6">
        <AnimatedSection>
          <div className="mb-5 flex flex-wrap gap-2">
            {providerScreenshots.map((entry, idx) => (
              <button
                key={entry.name}
                onClick={() => setActive(idx)}
                className={`rounded-full px-3.5 py-1.5 font-mono text-[11px] uppercase tracking-[0.1em] transition-colors ${
                  idx === active
                    ? "bg-teal text-ink"
                    : "border border-line text-muted hover:border-teal hover:text-teal"
                }`}
              >
                {entry.name}
              </button>
            ))}
          </div>
        </AnimatedSection>

        <AnimatedSection delay={0.04}>
          <div className="shell-card rounded-2xl p-4">
            <AnimatePresence mode="wait">
              <motion.div
                key={item.name}
                initial={reduced ? false : { opacity: 0, y: 8 }}
                animate={{ opacity: 1, y: 0 }}
                exit={{ opacity: 0, y: -8 }}
                transition={{ duration: 0.22 }}
              >
                <div className="mb-3 flex items-center justify-between gap-3 border-b border-line-soft pb-3">
                  <p className="font-mono text-[11px] uppercase tracking-[0.13em] text-teal">
                    {item.name}
                  </p>
                  <p className="text-sm text-muted">{item.desc}</p>
                </div>

                <div className="overflow-hidden rounded-xl border border-line-soft bg-ink">
                  <img
                    src={`${import.meta.env.BASE_URL}assets/${item.file}`}
                    alt={`${item.name} detail screenshot`}
                    className="block h-full w-full object-cover"
                  />
                </div>
              </motion.div>
            </AnimatePresence>
          </div>
        </AnimatedSection>
      </div>
    </section>
  );
}
