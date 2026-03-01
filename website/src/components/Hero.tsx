import { motion, useReducedMotion } from "framer-motion";
import CompositeDashboard from "./CompositeDashboard";

const keySignals = [
  { label: "Rendering", value: "Pure HTML/CSS/JS" },
  { label: "Canvas", value: "6 provider modules" },
  { label: "Parity", value: "Source dimensions mapped" },
  { label: "Theme", value: "Strict dark terminal" },
];

export default function Hero() {
  const reduced = useReducedMotion();

  return (
    <section className="hero" id="top">
      <div className="container hero-layout">
        <motion.div
          className="hero-copy"
          initial={reduced ? false : { opacity: 0, y: 14 }}
          animate={{ opacity: 1, y: 0 }}
          transition={{ duration: 0.4, ease: "easeOut" }}
        >
          <p className="eyebrow">OpenUsage / Terminal Replica</p>
          <h1>No images. Pixel-calibrated UI reconstruction in frontend code.</h1>
          <p className="hero-subtitle">
            The dashboard below is rendered from components only, with module dimensions and layout
            proportions aligned to source captures.
          </p>

          <div className="hero-actions">
            <a href="#modules" className="button button-solid">
              Inspect provider modules
            </a>
            <a href="#side-by-side" className="button button-ghost">
              Inspect side-by-side runtime
            </a>
          </div>

          <ul className="signal-list" aria-label="Replica signals">
            {keySignals.map((signal) => (
              <li key={signal.label}>
                <span>{signal.label}</span>
                <strong>{signal.value}</strong>
              </li>
            ))}
          </ul>
        </motion.div>

        <motion.div
          className="hero-stage"
          initial={reduced ? false : { opacity: 0, y: 18 }}
          animate={{ opacity: 1, y: 0 }}
          transition={{ duration: 0.5, ease: "easeOut", delay: 0.08 }}
        >
          <div className="stage-header">
            <p>openusage dashboard / 6 modules</p>
            <span>html replica</span>
          </div>
          <div className="stage-preview">
            <CompositeDashboard scale={0.18} />
          </div>
          <p className="stage-note">Component-rendered board · source geometry mapped</p>
        </motion.div>
      </div>
    </section>
  );
}
