import { motion, useReducedMotion, useInView } from "framer-motion";
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
  if (pct <= 35) return "#9de9bd";
  if (pct <= 55) return "#f2d28d";
  if (pct <= 75) return "#f0a870";
  return "#ff90b2";
}

const statusColor: Record<string, string> = {
  OK: "#9de9bd",
  WARN: "#f2d28d",
  LIMIT: "#ff90b2",
};

export default function DashboardGrid() {
  const reduced = useReducedMotion();
  const ref = useRef<HTMLDivElement>(null);
  const inView = useInView(ref, { once: true, margin: "-60px" });

  return (
    <div ref={ref} className="border border-[#1e2130] bg-[#0c0e14] text-[11px] font-mono overflow-hidden">
      {/* Top bar */}
      <div className="flex items-center justify-between px-3 py-1.5 border-b border-[#1e2130] text-[10px]">
        <div className="flex items-center gap-3">
          <span className="text-text font-medium">OpenUsage</span>
          <span className="text-dim">Tab: 1 &middot; 6 Compact 6 T providers</span>
        </div>
        <div className="flex items-center gap-3 text-dim">
          <span>? help</span>
          <span>q quit</span>
        </div>
      </div>

      {/* Grid */}
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 divide-x divide-y divide-[#1e2130]">
        {tiles.map((tile, i) => (
          <motion.div
            key={tile.name}
            initial={reduced ? false : { opacity: 0 }}
            animate={inView ? { opacity: 1 } : {}}
            transition={{ duration: 0.4, delay: 0.1 + i * 0.08 }}
            className="px-3 py-2.5 space-y-1.5 min-h-[140px]"
          >
            {/* Tile header */}
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-1.5">
                <span className="text-cyan">●</span>
                <span className="text-text">{tile.name}</span>
              </div>
              <span style={{ color: statusColor[tile.status] }} className="text-[10px]">
                {tile.status}
              </span>
            </div>

            {/* Gauges */}
            {tile.gauges.map((g) => (
              <div key={g.label} className="flex items-center gap-2">
                <span className="text-dim w-10 shrink-0 text-[10px]">{g.label}</span>
                <div className="flex-1 h-2 bg-[#161822] overflow-hidden">
                  <motion.div
                    className="h-full"
                    style={{ backgroundColor: barColor(g.pct) }}
                    initial={{ width: 0 }}
                    animate={inView ? { width: `${g.pct}%` } : {}}
                    transition={{
                      duration: reduced ? 0 : 0.8,
                      delay: 0.3 + i * 0.08,
                      ease: [0.25, 0.46, 0.45, 0.94],
                    }}
                  />
                </div>
                <span className="text-dim w-10 text-right tabular-nums text-[10px]">
                  {g.pct}%
                </span>
              </div>
            ))}

            {/* Top model */}
            <div className="text-[10px] text-dim truncate">
              <span className="text-[#2a3045]">1</span>{" "}
              <span className="text-text">{tile.topModel}</span>{" "}
              <span>{tile.topMetric}</span>
            </div>

            {/* Stat */}
            <div className="text-[10px] text-dim">{tile.stat}</div>
          </motion.div>
        ))}
      </div>

      {/* Bottom bar */}
      <div className="flex items-center justify-between px-3 py-1 border-t border-[#1e2130] text-[10px] text-[#2a3045]">
        <span>Tab switch view &middot; j/k navigate &middot; Enter detail</span>
        <span>t theme &middot; r refresh</span>
      </div>
    </div>
  );
}
