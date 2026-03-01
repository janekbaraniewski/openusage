interface Props {
  title: string;
}

export default function Section({ title }: Props) {
  return (
    <div className="flex items-center gap-1.5 mt-2.5 mb-1 text-[11px]">
      <span className="text-[#d8dcb6] whitespace-nowrap font-semibold">{title}</span>
      <div className="flex-1 border-b border-dotted border-[#2c3a58] translate-y-[1px]" />
    </div>
  );
}
