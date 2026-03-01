import { motion, useInView, useReducedMotion } from "framer-motion";
import { useRef } from "react";

interface Tile {
  name: string;
  status: "OK" | "WARN" | "LIMIT";
  gauges: { label: string; pct: number }[];
  topModel: string;
  topMetric: string;
  stat: string;
}

const tiles: Tile[] = [
  {
    name: "claude-code",
    status: "OK",
    gauges: [
      { label: "5h", pct: 3 },
      { label: "7d", pct: 48 },
    ],
    topModel: "claude-sonnet-4-6",
    topMetric: "39% 16.8M tok",
    stat: "~$51.93 today",
  },
  {
    name: "cursor-ide",
    status: "WARN",
    gauges: [
      { label: "Budget", pct: 43.7 },
      { label: "Cycle", pct: 48.7 },
    ],
    topModel: "deepseek-r2",
    topMetric: "16% 13.8M tok",
    stat: "$3212 / $3600",
  },
  {
    name: "openrouter",
    status: "OK",
    gauges: [{ label: "Credits", pct: 78.5 }],
    topModel: "moonshotai-kimi-k2-5",
    topMetric: "21% 15.9M tok",
    stat: "$7.85 / $10.00",
  },
  {
    name: "copilot",
    status: "OK",
    gauges: [
      { label: "Chat Q.", pct: 16.3 },
      { label: "Comp Q.", pct: 33.7 },
    ],
    topModel: "claude-sonnet-4-6",
    topMetric: "59% 14.4M tok",
    stat: "66.7k / 128k tok",
  },
  {
    name: "gemini-cli",
    status: "OK",
    gauges: [{ label: "Usage", pct: 52 }],
    topModel: "gemini-3-flash-preview",
    topMetric: "58% 11.1M tok",
    stat: "72% used",
  },
  {
    name: "codex-cli",
    status: "LIMIT",
    gauges: [
      { label: "5h", pct: 30 },
      { label: "7d", pct: 58 },
    ],
    topModel: "gpt-5-1-codex-max",
    topMetric: "32% 11.8M tok",
    stat: "221k / 258k tok",
  },
];

function barColor(pct: number): string {
  if (pct <= 35) return "#74f7c5";
  if (pct <= 55) return "#ffd58c";
  if (pct <= 75) return "#ffac74";
  return "#ff89ae";
}

const statusColor: Record<Tile["status"], string> = {
  OK: "#74f7c5",
  WARN: "#ffd58c",
  LIMIT: "#ff89ae",
};

export default function DashboardGrid() {
  const reduced = useReducedMotion();
  const ref = useRef<HTMLDivElement>(null);
  const inView = useInView(ref, { once: true, margin: "-60px" });

  return (
    <div ref={ref} className="dash-grid-shell">
      <div className="dash-grid-top">
        <div>
          <span>OpenUsage</span>
          <span>Tab: Compact providers</span>
        </div>
        <div>
          <span>? help</span>
          <span>q quit</span>
        </div>
      </div>

      <div className="dash-grid-body">
        {tiles.map((tile, i) => (
          <motion.article
            key={tile.name}
            className="dash-tile"
            initial={reduced ? false : { opacity: 0 }}
            animate={inView ? { opacity: 1 } : {}}
            transition={{ duration: 0.35, delay: 0.08 + i * 0.05 }}
          >
            <div className="dash-tile-head">
              <div>
                <span>●</span>
                <span>{tile.name}</span>
              </div>
              <span style={{ color: statusColor[tile.status] }}>{tile.status}</span>
            </div>

            {tile.gauges.map((g) => (
              <div key={g.label} className="dash-gauge-row">
                <span>{g.label}</span>
                <div>
                  <motion.div
                    style={{ backgroundColor: barColor(g.pct) }}
                    initial={{ width: 0 }}
                    animate={inView ? { width: `${g.pct}%` } : {}}
                    transition={{ duration: reduced ? 0 : 0.8, delay: 0.22 + i * 0.06 }}
                  />
                </div>
                <span>{g.pct}%</span>
              </div>
            ))}

            <p className="dash-model">
              {tile.topModel} <span>{tile.topMetric}</span>
            </p>
            <p className="dash-stat">{tile.stat}</p>
          </motion.article>
        ))}
      </div>

      <div className="dash-grid-bottom">
        <span>Tab switch view · j/k navigate · Enter detail</span>
        <span>t theme · r refresh</span>
      </div>
    </div>
  );
}
