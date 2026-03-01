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
  const pctStr = `${Math.round(Math.min(pct, 100))}%`;
  const value = /^\s*\d+%/.test(metric) ? metric : `${pctStr} ${metric}`;

  return (
    <div className="flex items-center gap-0 h-[18px] text-[11px] leading-none">
      <span className="w-[14px] text-right text-[#65708a] shrink-0">{rank}</span>
      <span
        className="w-[6px] h-[6px] mx-[5px] shrink-0"
        style={{ backgroundColor: c }}
      />
      <span className="text-[#aeb7c8] shrink-0 whitespace-nowrap max-w-[190px] truncate">
        {name}
      </span>
      <div className="flex-1 mx-1.5 border-b border-dotted border-[#293450] translate-y-[1px]" />
      <span className="text-[#d5db98] tabular-nums text-right shrink-0 whitespace-nowrap">
        {value}
      </span>
    </div>
  );
}
