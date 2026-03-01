import Panel from "../Panel";
import Gauge from "../Gauge";
import Section from "../Section";
import ModelRow from "../ModelRow";
import CodeStats from "../CodeStats";
import StatLine from "../StatLine";

interface ViewProps {
  bodyMinWidth?: number;
}

export default function CursorView({ bodyMinWidth = 580 }: ViewProps) {
  return (
    <Panel
      name="cursor-ide"
      status="OK"
      bodyMinWidth={bodyMinWidth}
      tabs={[
        { label: "Credits", active: true },
        { label: "Billing 11d17h" },
      ]}
      subtitle="○ Billing Feb 11 23:26-Mar 12 01:26 · Billing 11d17h"
    >
      <div className="text-[#7a829e] text-[10px] mb-0.5">Team Budget · Billing Cycle</div>
      <Gauge value={43.7} label="Team Budget" suffix="43.7%" />
      <Gauge value={48.7} label="Billing Cycle" suffix="48.7%" />
      <div className="text-[#7a829e] text-[10px] mt-0.5">your $277 · team $2996 · $388 remaining</div>
      <div className="text-[#7a829e] text-[10px]">$3212 / $3600 spent</div>

      <Section title="Model Burn (credits)" />
      <ModelRow rank={1} name="deepseek-r2" pct={16} metric="16% 13.8M tok · $496.64" />
      <ModelRow rank={2} name="claude-4.5-opus-high-thin." pct={14} metric="14% 13.0M tok · $443.53" />
      <ModelRow rank={3} name="claude-4.6-opus-thin." pct={14} metric="14% 12.2M tok · $368.80" />
      <ModelRow rank={4} name="gemini-3-flash" pct={4} metric="4% 12.2M tok · $142.28" />
      <ModelRow rank={5} name="Claude-4.5-opus-high-thin." pct={17} metric="17% 7.9M tok · $534.76" />
      <div className="flex gap-px items-end h-3 my-1 ml-[25px]">
        {[
          { h: 50, c: "#ff90b2" },
          { h: 70, c: "#f0a870" },
          { h: 65, c: "#f0a870" },
          { h: 40, c: "#f2d28d" },
          { h: 55, c: "#9de9bd" },
          { h: 30, c: "#7be3d6" },
        ].map((d, i) => (
          <div key={i} className="w-[5px]" style={{ height: `${d.h}%`, backgroundColor: d.c }} />
        ))}
      </div>
      <div className="text-[10px] text-[#2a3045] ml-[25px]">claude-4.5-... claude-4.6-...</div>
      <div className="text-[10px] text-[#2a3045] ml-[25px]">› 3 more models (Ctrl+0)</div>

      <Section title="Clients" />
      <ModelRow rank={1} name="Composer" pct={53} metric="53% 5.1k req" />
      <ModelRow rank={2} name="Human" pct={41} metric="41% 3.9k req" />
      <ModelRow rank={3} name="Tab Completion" pct={5} metric="5% 494 req" />
      <ModelRow rank={4} name="CLI Agents" pct={0} metric="0% 42 req" />

      <Section title="Tool Usage" />
      <ModelRow rank={1} name="completion_resolve" pct={2} metric="2% 6.0k" />
      <ModelRow rank={2} name="semantic_tokens" pct={2} metric="2% 5.8k" />
      <ModelRow rank={3} name="mcp_terraform (mcp)" pct={2} metric="2% 5.7k" />
      <ModelRow rank={4} name="mcp_jira (mcp)" pct={2} metric="2% 5.6k" />
      <ModelRow rank={5} name="run_terminal_cmd" pct={2} metric="2% 5.6k" />
      <ModelRow rank={6} name="workspace_symbol" pct={2} metric="2% 5.6k" />
      <div className="text-[10px] text-[#2a3045] ml-[25px]">› 6 more tools (Ctrl+0)</div>

      <Section title="Language (requests)" />
      <ModelRow rank={1} name=".ini" pct={6} metric="6% 5.6k req" />
      <ModelRow rank={2} name="yaml" pct={5} metric="5% 5.4k" />
      <ModelRow rank={3} name="md" pct={6} metric="6% 5.5k" />
      <ModelRow rank={4} name="go" pct={6} metric="6% 5.4k req" />
      <ModelRow rank={5} name="rs" pct={6} metric="6% 5.3k req" />
      <ModelRow rank={6} name="hcl" pct={6} metric="6% 5.2k req" />
      <div className="text-[10px] text-[#2a3045] ml-[25px]">› more languages (Ctrl+0)</div>

      <Section title="Code Statistics" />
      <CodeStats
        added={139}
        removed={335}
        filesChanged="2.2k files"
        commits="3.6k commits · 65% AI"
        prompts="3.8k total"
      />

      <div className="mt-2 pt-2 border-t border-[#1e2130] space-y-0.5">
        <StatLine label="Credits" value="plan $3.45/$20.00 · cap $3212/$3600 · mine $216.56 · billing $488.84 · today $448.90" />
        <StatLine label="Team" value="members 51 · owners 3.4k owners" />
        <StatLine label="Usage" value="used 31% · auto 37% · api 88% · ctx 22k" />
        <StatLine label="Activity" value="today 720 · all 1.0k · sess 738 sessions · reqs 5.3k" />
        <StatLine label="Lines" value="comp 280 · comp sug 2 · tab 98 · tab sug 253" />
      </div>

      <div className="mt-1.5 space-y-0.5">
        <StatLine label="AI Deleted" value="3955 files" />
        <StatLine label="AI Tracked" value="2365 files" />
        <StatLine label="Billing Input Tokens" value="3.9M tok" />
        <StatLine label="Billing Output Tokens" value="0.6M tok" />
        <StatLine label="Plan Bonus" value="$207.32" />
        <StatLine label="Plan Included" value="$207.18" />
      </div>

      <div className="mt-1.5 text-[#7a829e] text-[10px]">Team — $3.45 / $3600 team spend ($387.79 remaining)</div>

      <div className="mt-2 pt-2 border-t border-[#1e2130] space-y-0.5 text-[10px]">
        <StatLine label="Account" value="demo.user@acme-corp.dev" />
        <StatLine label="Plan" value="Team" />
        <StatLine label="Team" value="acme" />
      </div>
    </Panel>
  );
}
