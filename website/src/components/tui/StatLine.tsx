interface Props {
  label: string;
  value: string;
}

export default function StatLine({ label, value }: Props) {
  return (
    <div className="flex items-center h-[17px] text-[11px] leading-none">
      <span className="text-[#8c96ae] shrink-0">{label}</span>
      <div className="flex-1 mx-1.5 border-b border-dotted border-[#2a3552] translate-y-[1px]" />
      <span className="text-[#bcc5d3] tabular-nums text-right whitespace-nowrap">{value}</span>
    </div>
  );
}
