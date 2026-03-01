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
        <span className="text-[#aeb7c8] shrink-0 whitespace-nowrap text-[11px] mr-1.5">
          {label}
        </span>
      )}
      <div className="flex-1 h-[6px] bg-[#121c2e] relative">
        <div className="absolute inset-0 [background-image:radial-gradient(circle,rgba(93,111,142,0.35)_1px,transparent_1px)] [background-size:4px_4px]" />
        <div
          className="absolute inset-y-0 left-0"
          style={{ width: `${pct}%`, backgroundColor: barColor(pct) }}
        />
      </div>
      {suffix !== undefined && (
        <span className="text-[#d5db98] text-[11px] tabular-nums text-right shrink-0 whitespace-nowrap ml-1.5">
          {suffix}
        </span>
      )}
    </div>
  );
}
