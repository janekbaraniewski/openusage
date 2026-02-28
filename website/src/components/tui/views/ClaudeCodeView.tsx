import Panel from "../Panel";
import Gauge from "../Gauge";
import Section from "../Section";
import ModelRow from "../ModelRow";
import CodeStats from "../CodeStats";
import StatLine from "../StatLine";

export default function ClaudeCodeView() {
  return (
    <Panel
      name="claude-code"
      status="OK"
      tabs={[
        { label: "Usage", active: true },
        { label: "Usage 5h" },
        { label: "Usage 5h 2h03m" },
        { label: "Usage 7d 3d22h" },
        { label: "Usage 7d 1h54m" },
      ]}
      subtitle="○ Usage 5h Feb 27 21:08-Feb 28 02:08"
    >
      <Gauge value={3} label="Usage 5h" suffix="3.0%" />
      <Gauge value={48} label="Usage 7d" suffix="48.0%" />
      <div className="text-[#7a829e] text-[10px] mt-0.5">
        5h 3% &middot; 7d 48%
      </div>
      <div className="text-[#7a829e] text-[10px]">
        ~$51.93 today &middot; $651.62/h
      </div>

      <Section title="Model Burn (credits)" />
      <ModelRow rank={1} name="claude-sonnet-4-6" pct={39} metric="39% 16.8M tok · $449.67" />
      <ModelRow rank={2} name="claude-opus-4-6" pct={59} metric="59% 11.4M tok · $691.66" />
      <ModelRow rank={3} name="claude-haiku-4-5-20251001" pct={2} metric="2% 10.4M tok · $22.21" />
      {/* Mini trend */}
      <div className="flex gap-px items-end h-3 my-1 ml-[25px]">
        {[
          { h: 60, c: "#ff90b2" }, { h: 45, c: "#ff90b2" }, { h: 80, c: "#f0a870" },
          { h: 90, c: "#f0a870" }, { h: 30, c: "#f0a870" }, { h: 20, c: "#f2d28d" },
          { h: 15, c: "#f2d28d" },
        ].map((d, i) => (
          <div key={i} className="w-[5px]" style={{ height: `${d.h}%`, backgroundColor: d.c }} />
        ))}
      </div>
      <div className="text-[10px] text-[#2a3045] ml-[25px]">
        claude-opus... claude-haiku...
      </div>
      <div className="text-[10px] text-[#2a3045] ml-[25px]">
        › 2 more models (Ctrl+0)
      </div>

      <Section title="Clients" />
      <ModelRow rank={1} name="Docs Site" pct={23} metric="23% 8.7M tok · 133 req" />
      <ModelRow rank={2} name="Infra Config" pct={21} metric="21% 7.7M tok · 3.1k req" />
      <ModelRow rank={3} name="Test Suite" pct={21} metric="21% 7.8M tok · 4.5k req" />
      <ModelRow rank={4} name="API Gateway" pct={13} metric="13% 4.1M tok · 1.6k req" />
      {/* Mini trend */}
      <div className="flex gap-px items-end h-3 my-1 ml-[25px]">
        {[
          { h: 70, c: "#ff90b2" }, { h: 55, c: "#f0a870" }, { h: 50, c: "#f0a870" },
          { h: 40, c: "#f2d28d" }, { h: 20, c: "#9de9bd" },
        ].map((d, i) => (
          <div key={i} className="w-[5px]" style={{ height: `${d.h}%`, backgroundColor: d.c }} />
        ))}
      </div>
      <div className="text-[10px] text-[#2a3045] ml-[25px]">
        Docs Site &middot; Infra Config
      </div>
      <div className="text-[10px] text-[#2a3045] ml-[25px]">
        › 5 more clients (Ctrl+0)
      </div>

      <Section title="Tool Usage" />
      <ModelRow rank={1} name="todowrite" pct={11} metric="11% 6.0k" />
      <ModelRow rank={2} name="edit" pct={11} metric="11% 5.9k" />
      <ModelRow rank={3} name="shell" pct={11} metric="11% 5.8k" />
      <ModelRow rank={4} name="notebookedit" pct={10} metric="10% 5.6k" />
      <ModelRow rank={5} name="calls_today" pct={10} metric="10% 5.4k" />
      <ModelRow rank={6} name="write" pct={8} metric="8% 4.5k" />
      <div className="text-[10px] text-[#2a3045] ml-[25px]">
        › 5 more tools (Ctrl+0)
      </div>

      <Section title="Language (requests)" />
      <ModelRow rank={1} name="go" pct={61} metric="61% 5.2k req" />
      <ModelRow rank={2} name="python" pct={14} metric="14% 4.6k req" />
      <ModelRow rank={3} name="typescript" pct={14} metric="14% 4.6k req" />
      <ModelRow rank={4} name="rust" pct={6} metric="6% 3.4k req" />
      <ModelRow rank={5} name="terraform" pct={4} metric="4% 4.5k req" />
      <ModelRow rank={6} name="yaml" pct={11} metric="11% 4.2k req" />
      <div className="text-[10px] text-[#2a3045] ml-[25px]">
        › more languages (Ctrl+0)
      </div>

      <Section title="Code Statistics" />
      <CodeStats
        added={454}
        removed={224}
        filesChanged="4.2k files"
        commits="1.8k commits · 44% AI"
        prompts="2.3k total"
      />

      <div className="mt-2 pt-2 border-t border-[#1e2130] space-y-0.5">
        <StatLine label="Credits" value="today $51.93 · 5h $85.17 · 7d $592.58 · all $227.90" />
        <StatLine label="Usage" value="msgs 1.5k · sess 575 sessions" />
        <StatLine label="" value="5h 3% · 7d 48% · opus 59%" />
        <StatLine label="" value="tools 4.5k calls · 7d tools 3.4k calls" />
        <StatLine label="Tokens" value="in 6.4M · out 4.0M · 7d in 7.5M · 7d out 61.0k" />
        <StatLine label="Lines" value="added 454 · removed 224 · files 4.2k" />
        <StatLine label="" value="commits 1.8k · prompts 2.3k prompts" />
        <StatLine label="Burn Rate" value="651.62 USD/h" />
        <StatLine label="" value="-$316.69 today · -$570.85/h" />
      </div>

      <div className="mt-2 pt-2 border-t border-[#1e2130] space-y-0.5 text-[10px]">
        <StatLine label="Account" value="demo.user@example.test" />
        <StatLine label="Type" value="max_5" />
      </div>
    </Panel>
  );
}
