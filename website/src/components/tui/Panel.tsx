import type { ReactNode } from "react";

interface Props {
  name: string;
  status?: "OK" | "WARN" | "LIMIT";
  tabs?: { label: string; active?: boolean }[];
  subtitle?: string;
  children: ReactNode;
  className?: string;
  bodyMinWidth?: number;
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
  bodyMinWidth = 580,
}: Props) {
  return (
    <div
      className={`relative w-full border border-[#6b4f67] bg-[#0a111d] font-mono text-[11px] leading-[1.45] overflow-hidden select-none ${className}`}
    >
      <div className="flex items-center justify-between px-2.5 h-[26px] border-b border-[#1e2130]">
        <div className="flex items-center gap-1.5 min-w-0">
          <span className="text-[#f0a870] text-[9px]">●</span>
          <span className="text-[#aeb7c8] font-medium">{name}</span>
        </div>
        <span className="text-[10px]" style={{ color: statusColors[status] }}>
          {status}
        </span>
      </div>

      {tabs && (
        <div className="flex items-center gap-2.5 px-2.5 h-[22px] border-b border-[#1e2130] overflow-x-auto text-[10px]">
          {tabs.map((tab) => (
            <span
              key={tab.label}
              className={tab.active ? "text-[#f2d28d]" : "text-[#5f6984]"}
            >
              {tab.active ? "◉" : "○"} {tab.label}
            </span>
          ))}
        </div>
      )}

      {subtitle && (
        <div className="px-2.5 h-[20px] flex items-center border-b border-[#1e2130] text-[10px] text-[#616d8a] overflow-hidden">
          {subtitle}
        </div>
      )}

      <div className="overflow-x-auto relative">
        <div className="absolute inset-0 pointer-events-none opacity-55 [background-image:repeating-linear-gradient(180deg,rgba(44,58,89,0.55)_0,rgba(44,58,89,0.55)_1px,transparent_1px,transparent_18px)]" />
        <div className="w-full px-2.5 py-1.5" style={{ minWidth: `${bodyMinWidth}px` }}>
          {children}
        </div>
      </div>
    </div>
  );
}
