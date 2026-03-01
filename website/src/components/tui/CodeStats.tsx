interface Props {
  added: number;
  removed: number;
  filesChanged: string;
  commits: string;
  prompts: string;
}

export default function CodeStats({ added, removed, filesChanged, commits, prompts }: Props) {
  const total = added + removed;
  const addPct = total > 0 ? (added / total) * 100 : 50;

  return (
    <div className="text-[11px] leading-none space-y-[3px]">
      <div className="flex items-center h-[17px] gap-1">
        <span className="text-[#9de9bd] whitespace-nowrap">+{added} added</span>
        <div className="flex-1 h-[6px] flex overflow-hidden">
          <div className="h-full bg-[#9de9bd]" style={{ width: `${addPct}%` }} />
          <div className="h-full bg-[#ff90b2]" style={{ width: `${100 - addPct}%` }} />
        </div>
        <span className="text-[#ff90b2] whitespace-nowrap">-{removed} removed</span>
      </div>
      <div className="flex items-center h-[17px]">
        <span className="text-[#8c96ae]">Files Changed</span>
        <div className="flex-1 mx-1.5 border-b border-dotted border-[#2a3552] translate-y-[1px]" />
        <span className="text-[#bcc5d3] tabular-nums">{filesChanged}</span>
      </div>
      <div className="flex items-center h-[17px]">
        <span className="text-[#8c96ae]">Commits</span>
        <div className="flex-1 mx-1.5 border-b border-dotted border-[#2a3552] translate-y-[1px]" />
        <span className="text-[#bcc5d3] tabular-nums">{commits}</span>
      </div>
      <div className="flex items-center h-[17px]">
        <span className="text-[#8c96ae]">Prompts</span>
        <div className="flex-1 mx-1.5 border-b border-dotted border-[#2a3552] translate-y-[1px]" />
        <span className="text-[#bcc5d3] tabular-nums">{prompts}</span>
      </div>
    </div>
  );
}
