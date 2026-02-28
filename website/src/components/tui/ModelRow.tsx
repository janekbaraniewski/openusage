interface Props {
  rank: number;
  name: string;
  pct: number;
  metric: string;
  color?: string;
}

const RANK_COLORS = [
  "#ff90b2", // 1 - pink
  "#f0a870", // 2 - orange
  "#f2d28d", // 3 - yellow
  "#9de9bd", // 4 - green
  "#7be3d6", // 5 - cyan
  "#c8a8ff", // 6 - purple
];

export default function ModelRow({ rank, name, pct, metric, color }: Props) {
  const c = color || RANK_COLORS[(rank - 1) % RANK_COLORS.length];

  return (
    <div className="flex items-center gap-0 h-[18px] text-[11px] leading-none">
      {/* Rank */}
      <span className="w-[14px] text-right text-[#2a3045] shrink-0">{rank}</span>
      {/* Color indicator */}
      <span
        className="w-[6px] h-[6px] mx-[5px] shrink-0"
        style={{ backgroundColor: c }}
      />
      {/* Name */}
      <span className="text-[#d0d0d0] shrink-0 whitespace-nowrap max-w-[180px] truncate">
        {name}
      </span>
      {/* Gauge bar - fills remaining space */}
      <div className="flex-1 h-[5px] bg-[#161822] mx-1.5 min-w-[30px]">
        <div
          className="h-full"
          style={{ width: `${Math.min(pct, 100)}%`, backgroundColor: c }}
        />
      </div>
      {/* Metric */}
      <span className="text-[#7a829e] tabular-nums text-right shrink-0 whitespace-nowrap">
        {metric}
      </span>
    </div>
  );
}
