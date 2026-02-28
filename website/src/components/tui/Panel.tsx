import type { ReactNode } from "react";

interface Props {
  name: string;
  status?: "OK" | "WARN" | "LIMIT";
  tabs?: { label: string; active?: boolean }[];
  subtitle?: string;
  children: ReactNode;
  className?: string;
}

const statusColors: Record<string, string> = {
  OK: "#9de9bd",
  WARN: "#f2d28d",
  LIMIT: "#ff90b2",
};

export default function Panel({
  name,
  status = "OK",
  tabs,
  subtitle,
  children,
  className = "",
}: Props) {
  return (
    <div
      className={`border border-[#1e2130] bg-[#0c0e14] font-mono text-[11px] leading-[1.55] overflow-hidden select-none ${className}`}
    >
      {/* Header row */}
      <div className="flex items-center justify-between px-2.5 h-[26px] border-b border-[#1e2130]">
        <div className="flex items-center gap-1.5 min-w-0">
          <span className="text-[#7be3d6] text-[9px]">●</span>
          <span className="text-[#d0d0d0] font-medium">{name}</span>
        </div>
        <span className="text-[10px]" style={{ color: statusColors[status] }}>
          {status}
        </span>
      </div>

      {/* Tab bar */}
      {tabs && (
        <div className="flex items-center gap-2.5 px-2.5 h-[22px] border-b border-[#1e2130] overflow-x-auto text-[10px]">
          {tabs.map((tab) => (
            <span
              key={tab.label}
              className={tab.active ? "text-[#7be3d6]" : "text-[#2a3045]"}
            >
              {tab.active ? "◆" : "○"} {tab.label}
            </span>
          ))}
        </div>
      )}

      {/* Subtitle */}
      {subtitle && (
        <div className="px-2.5 h-[20px] flex items-center border-b border-[#1e2130] text-[10px] text-[#2a3045] overflow-hidden">
          {subtitle}
        </div>
      )}

      {/* Body - scrollable on narrow screens to maintain terminal proportions */}
      <div className="overflow-x-auto">
        <div className="px-2.5 py-1.5 min-w-[580px]">{children}</div>
      </div>
    </div>
  );
}
