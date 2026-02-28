interface Props {
  label: string;
  value: string;
}

export default function StatLine({ label, value }: Props) {
  return (
    <div className="flex items-center h-[17px] text-[11px] leading-none">
      <span className="text-[#7a829e] shrink-0">{label}</span>
      <span className="flex-1" />
      <span className="text-[#d0d0d0] tabular-nums text-right whitespace-nowrap">{value}</span>
    </div>
  );
}
