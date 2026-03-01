import Panel from "../Panel";
import Gauge from "../Gauge";
import Section from "../Section";
import ModelRow from "../ModelRow";
import CodeStats from "../CodeStats";
import StatLine from "../StatLine";

interface ViewProps {
  bodyMinWidth?: number;
}

export default function GeminiView({ bodyMinWidth = 580 }: ViewProps) {
  return (
    <Panel
      name="gemini-cli"
      status="OK"
      bodyMinWidth={bodyMinWidth}
      tabs={[
        { label: "Usage", active: true },
        { label: "Quota Model Gemini 2 5 Pro Requests 1d02h" },
      ]}
    >
      <Gauge value={52} label="Usage (Worst…)" suffix="52.0%" />
      <div className="text-[#7a829e] text-[10px] mt-0.5">72% usage used</div>

      <Section title="Model Burn (tokens)" />
      <ModelRow rank={1} name="gemini-3-flash-preview" pct={58} metric="58% 11.1M tok" />
      <ModelRow rank={2} name="gemini-3-pro" pct={42} metric="42% 8.0M tok" />

      <Section title="Clients" />
      <ModelRow rank={1} name="CLI Agents" pct={100} metric="100% 6.1M tok · 2.6k sess" />
      <div className="text-[10px] text-[#2a3045] ml-[25px]">Trend (daily by client)</div>
      <div className="text-[10px] text-[#2a3045] ml-[25px]">CLI Agents</div>

      <Section title="Tool Usage" />
      <div className="text-[#7a829e] text-[10px] -mt-0.5 mb-0.5 ml-[25px]">20.7k calls · 54% ok</div>
      <ModelRow rank={1} name="read_file" pct={24} metric="24% 4.9k" />
      <ModelRow rank={2} name="google_web_search" pct={23} metric="23% 4.8k" />
      <ModelRow rank={3} name="run_shell_command" pct={17} metric="17% 3.5k" />
      <ModelRow rank={4} name="calls_success" pct={17} metric="17% 3.5k" />
      <ModelRow rank={5} name="write_file" pct={8} metric="8% 1.6k" />
      <ModelRow rank={6} name="calls_today" pct={7} metric="7% 1.5k" />
      <div className="text-[10px] text-[#2a3045] ml-[25px]">› 1 more tools (Ctrl+0)</div>

      <Section title="Language (requests)" />
      <ModelRow rank={1} name="typescript" pct={50} metric="50% 5.7k req" />
      <ModelRow rank={2} name="markdown" pct={42} metric="42% 4.7k req" />
      <ModelRow rank={3} name="yaml" pct={7} metric="7% 822 req" />
      <ModelRow rank={4} name="go" pct={0} metric="0% 50 req" />

      <Section title="Code Statistics" />
      <CodeStats
        added={162}
        removed={174}
        filesChanged="1.6k files"
        commits="4.0k commits · 20% AI"
        prompts="4.0k total"
      />

      <Section title="Daily Usage" />
      <div className="text-[10px] text-[#7a829e] ml-[25px]">Req</div>
      <div className="text-[10px] text-[#7a829e] ml-[25px] -mt-0.5">Tokens</div>

      <div className="mt-1 pt-1 border-t border-[#1e2130] space-y-0.5">
        <StatLine label="Usage" value="all 52% · exhausted 88 · models 15 models · 30 models" />
        <StatLine label="Activity" value="msgs 436 · sess 5.3k sessions · tools 1.5k calls · conv 133" />
        <StatLine label="Tokens" value="today tok 5.3M · 7d tok 5.2M · in 7.9M · out 4.5M" />
        <StatLine label="" value="cached 2.3M · reason 7.9M · tools 917.8k" />
        <StatLine label="7d Tok" value="in 6.5M · out 6.7M · reason 3.9M · tools 2.6M" />
        <StatLine label="Tools" value="all 4.7k calls · ok 3.4k calls · err 2.8k calls · cancel 1.6k calls · ok % 54%" />
        <StatLine label="Lines" value="added 162 · removed 174 · files 1.6k files · commits 4.0k commits · prompts 4.0k prompts" />
      </div>

      <div className="mt-1.5 space-y-0.5">
        <StatLine label="Other Usage" value="" />
        <StatLine label="gemini-2-5-flash-lite req" value="52.7% used · 8h19m" />
        <StatLine label="gemini-2-0-flash req" value="35.0% used · 8h47m" />
      </div>

      <div className="mt-1.5 space-y-0.5">
        <StatLine label="7d Messages" value="4558 messages" />
        <StatLine label="7d Sessions" value="3770 sessions" />
        <StatLine label="7d Tool Calls" value="4382 calls" />
      </div>

      <div className="mt-2 pt-2 border-t border-[#1e2130] space-y-0.5 text-[10px]">
        <StatLine label="Version" value="0.4.21" />
      </div>
    </Panel>
  );
}
