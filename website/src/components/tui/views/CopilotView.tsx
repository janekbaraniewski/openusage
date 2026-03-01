import Panel from "../Panel";
import Gauge from "../Gauge";
import Section from "../Section";
import ModelRow from "../ModelRow";
import CodeStats from "../CodeStats";
import StatLine from "../StatLine";

interface ViewProps {
  bodyMinWidth?: number;
}

export default function CopilotView({ bodyMinWidth = 580 }: ViewProps) {
  return (
    <Panel
      name="copilot"
      status="OK"
      bodyMinWidth={bodyMinWidth}
      tabs={[
        { label: "Usage", active: true },
        { label: "Core 29m" },
        { label: "Search 1m" },
        { label: "GraphQL 1h02m" },
        { label: "Usage 23d13h" },
      ]}
    >
      <Gauge value={16.3} label="Chat Quota" suffix="16.3%" />
      <Gauge value={33.7} label="Completions Q." suffix="33.7%" />
      <div className="text-[#7a829e] text-[10px] mt-0.5">48% usage used · 66.7k / 128.0k tokens</div>

      <Section title="Model Burn (credits)" />
      <ModelRow rank={1} name="claude-sonnet-4-6" pct={59} metric="59% 14.4M tok · $520.22" />
      <ModelRow rank={2} name="gpt-5-mini" pct={41} metric="41% 11.2M tok · $366.96" />

      <Section title="Clients" />
      <ModelRow rank={1} name="Vscode" pct={60} metric="60% 7.6M tok · 4.0k sess" />
      <ModelRow rank={2} name="JetBrains" pct={20} metric="20% 2.5M tok · 4.0k sess" />
      <ModelRow rank={3} name="CLI Agents" pct={20} metric="20% 2.5M tok · 3.2k sess" />
      <div className="flex gap-px items-end h-3 my-1 ml-[25px]">
        {[
          { h: 85, c: "#ff90b2" },
          { h: 40, c: "#f0a870" },
          { h: 35, c: "#f2d28d" },
        ].map((d, i) => (
          <div key={i} className="w-[5px]" style={{ height: `${d.h}%`, backgroundColor: d.c }} />
        ))}
      </div>
      <div className="text-[10px] text-[#2a3045] ml-[25px]">Vscode · JetBrains</div>

      <Section title="Tool Usage" />
      <ModelRow rank={1} name="task" pct={10} metric="10% 5.7k" />
      <ModelRow rank={2} name="bash" pct={9} metric="9% 5.4k" />
      <ModelRow rank={3} name="glob" pct={9} metric="9% 5.3k" />
      <ModelRow rank={4} name="bash_calls" pct={9} metric="9% 5.2k" />
      <ModelRow rank={5} name="edit" pct={8} metric="8% 4.8k" />
      <ModelRow rank={6} name="glob_calls" pct={8} metric="8% 4.8k" />
      <div className="text-[10px] text-[#2a3045] ml-[25px]">› 11 more tools (Ctrl+0)</div>

      <Section title="Language (requests)" />
      <ModelRow rank={1} name="typescript" pct={61} metric="61% 5.2k req" />
      <ModelRow rank={2} name="go" pct={18} metric="18% 1.5k req" />
      <ModelRow rank={3} name="sql" pct={16} metric="16% 1.4k req" />
      <ModelRow rank={4} name="yaml" pct={5} metric="5% 402 req" />

      <Section title="Code Statistics" />
      <CodeStats
        added={383}
        removed={145}
        filesChanged="4.3k files"
        commits="2.7k commits · 81% AI"
        prompts="1.1k total"
      />

      <div className="mt-2 pt-2 border-t border-[#1e2130] space-y-0.5">
        <StatLine label="Credits" value="chat 49/300 · comp 101/300 · premium 28/50" />
        <StatLine label="Usage" value="ctx 66.7k/128.0k · 7.3M · 7d tok 6.2M" />
        <StatLine label="Rate" value="core 4.5k/5.0k · search 21/30 · graphql 4.3k/5.0k" />
        <StatLine label="Activity" value="msgs 5.0k · sess 2.2k sessions · tools 2.8k calls · prompts 1.1k prompts" />
        <StatLine label="Tokens" value="cli in 7.2M" />
        <StatLine label="Lines" value="added 383 · removed 145 · files 4.3k · commits 2.7k commits" />
        <StatLine label="Seats" value="demo_ac... 34 · demo_to... 53" />
      </div>

      <div className="mt-1.5 space-y-0.5">
        <StatLine label="7d Messages" value="1555 messages" />
        <StatLine label="Cli Tokens" value="4.6M tok" />
        <StatLine label="Cli Total Calls" value="2310 calls" />
      </div>

      <div className="mt-2 pt-2 border-t border-[#1e2130] space-y-0.5 text-[10px]">
        <StatLine label="Account" value="demo.user@example.test" />
        <StatLine label="Plan" value="copilot_for_business" />
      </div>
    </Panel>
  );
}
