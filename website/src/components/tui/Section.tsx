interface Props {
  title: string;
}

export default function Section({ title }: Props) {
  return (
    <div className="flex items-center gap-1.5 mt-2.5 mb-1 text-[11px]">
      <span className="text-[#7a829e] whitespace-nowrap">{title}</span>
      <div className="flex-1 h-px bg-[#1e2130]" />
    </div>
  );
}
