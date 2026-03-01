import { useEffect, useMemo, useState } from "react";
import { AnimatePresence, motion, useReducedMotion } from "framer-motion";
import AnimatedSection from "./ui/AnimatedSection";
import { providerScreenshots } from "../data/providers";

type ViewMode = "fit" | "actual";

const dimensions: Record<string, { width: number; height: number }> = {
  "claudecode.png": { width: 1434, height: 2084 },
  "cursor.png": { width: 1432, height: 2220 },
  "openrouter.png": { width: 1440, height: 2092 },
  "copilot.png": { width: 1432, height: 1786 },
  "gemini.png": { width: 1432, height: 1932 },
  "codex.png": { width: 1442, height: 2188 },
};

export default function ProviderReplicaShowcase() {
  const reduced = useReducedMotion();
  const [active, setActive] = useState(0);
  const [mode, setMode] = useState<ViewMode>("fit");

  useEffect(() => {
    if (reduced || mode === "actual") {
      return undefined;
    }

    const timer = window.setInterval(() => {
      setActive((prev) => (prev + 1) % providerScreenshots.length);
    }, 7800);

    return () => window.clearInterval(timer);
  }, [reduced, mode]);

  const selected = providerScreenshots[active];
  const selectedSize = useMemo(() => dimensions[selected.file], [selected.file]);

  return (
    <section className="section section-replica" id="providers">
      <div className="container">
        <AnimatedSection className="section-head">
          <p className="section-label">Provider Replica Deck</p>
          <h2>Exact copies of real OpenUsage provider modules.</h2>
          <p>
            These are direct high-resolution captures from the app. Switch providers and inspect each
            module in fit mode or 1:1 pixel mode.
          </p>
        </AnimatedSection>

        <div className="replica-shell">
          <div className="replica-sidebar" role="tablist" aria-label="Provider modules">
            {providerScreenshots.map((entry, index) => {
              const entrySize = dimensions[entry.file];

              return (
                <button
                  key={entry.file}
                  type="button"
                  role="tab"
                  aria-selected={index === active}
                  className={index === active ? "is-active" : ""}
                  onClick={() => setActive(index)}
                >
                  <span>{entry.name}</span>
                  <small>{entrySize.width}x{entrySize.height}</small>
                </button>
              );
            })}
          </div>

          <div className="replica-main">
            <div className="replica-head">
              <div>
                <p>{selected.name}</p>
                <span>{selected.desc}</span>
              </div>

              <div className="replica-tools" role="radiogroup" aria-label="Preview scale mode">
                <button
                  type="button"
                  role="radio"
                  aria-checked={mode === "fit"}
                  className={mode === "fit" ? "is-active" : ""}
                  onClick={() => setMode("fit")}
                >
                  Fit
                </button>
                <button
                  type="button"
                  role="radio"
                  aria-checked={mode === "actual"}
                  className={mode === "actual" ? "is-active" : ""}
                  onClick={() => setMode("actual")}
                >
                  1:1 Pixels
                </button>
              </div>
            </div>

            <div className={`replica-viewport ${mode === "actual" ? "is-actual" : ""}`}>
              <AnimatePresence mode="wait">
                <motion.figure
                  key={selected.file + mode}
                  className="replica-image-wrap"
                  initial={reduced ? false : { opacity: 0, y: 10 }}
                  animate={{ opacity: 1, y: 0 }}
                  exit={reduced ? undefined : { opacity: 0, y: -8 }}
                  transition={{ duration: 0.24, ease: "easeOut" }}
                >
                  <img
                    src={`${import.meta.env.BASE_URL}assets/${selected.file}`}
                    alt={`${selected.name} module screenshot`}
                    className={mode === "actual" ? "replica-image-actual" : "replica-image-fit"}
                  />
                </motion.figure>
              </AnimatePresence>
            </div>

            <p className="replica-footnote">
              Capture: {selectedSize.width}x{selectedSize.height}px · mode: {mode === "fit" ? "scaled to viewport" : "native pixels"}
            </p>
          </div>
        </div>
      </div>
    </section>
  );
}
