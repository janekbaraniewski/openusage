interface Props {
  value: number;
  label?: string;
  suffix?: string;
}

function barColor(pct: number): string {
  if (pct <= 30) return "#9de9bd";
  if (pct <= 50) return "#f2d28d";
  if (pct <= 75) return "#f0a870";
  return "#ff90b2";
}

export default function Gauge({ value, label, suffix }: Props) {
  const pct = Math.min(value, 100);

  return (
    <div className="flex items-center gap-0 leading-none h-[18px]">
      {label && (
        <span className="text-[#7a829e] shrink-0 whitespace-nowrap text-[11px] mr-1.5">
          {label}
        </span>
      )}
      <div className="flex-1 h-[6px] bg-[#161822] relative">
        <div
          className="absolute inset-y-0 left-0"
          style={{ width: `${pct}%`, backgroundColor: barColor(pct) }}
        />
      </div>
      {suffix !== undefined && (
        <span className="text-[#d0d0d0] text-[11px] tabular-nums text-right shrink-0 whitespace-nowrap ml-1.5">
          {suffix}
        </span>
      )}
    </div>
  );
}
