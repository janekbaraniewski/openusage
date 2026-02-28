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
      {/* Add/remove bar */}
      <div className="flex items-center h-[17px] gap-1">
        <span className="text-[#9de9bd] whitespace-nowrap">+{added} added</span>
        <div className="flex-1 h-[6px] flex overflow-hidden">
          <div className="h-full bg-[#9de9bd]" style={{ width: `${addPct}%` }} />
          <div className="h-full bg-[#ff90b2]" style={{ width: `${100 - addPct}%` }} />
        </div>
        <span className="text-[#ff90b2] whitespace-nowrap">-{removed} removed</span>
      </div>
      <div className="flex items-center h-[17px]">
        <span className="text-[#7a829e]">Files Changed</span>
        <span className="flex-1" />
        <span className="text-[#d0d0d0] tabular-nums">{filesChanged}</span>
      </div>
      <div className="flex items-center h-[17px]">
        <span className="text-[#7a829e]">Commits</span>
        <span className="flex-1" />
        <span className="text-[#d0d0d0] tabular-nums">{commits}</span>
      </div>
      <div className="flex items-center h-[17px]">
        <span className="text-[#7a829e]">Prompts</span>
        <span className="flex-1" />
        <span className="text-[#d0d0d0] tabular-nums">{prompts}</span>
      </div>
    </div>
  );
}
