import Panel from "../Panel";
import Gauge from "../Gauge";
import Section from "../Section";
import ModelRow from "../ModelRow";
import CodeStats from "../CodeStats";
import StatLine from "../StatLine";

interface ViewProps {
  bodyMinWidth?: number;
}

export default function CodexView({ bodyMinWidth = 580 }: ViewProps) {
  return (
    <Panel
      name="codex-cli"
      status="LIMIT"
      bodyMinWidth={bodyMinWidth}
      tabs={[
        { label: "Usage", active: true },
        { label: "Primary 5h28m" },
        { label: "Secondary 3d00h" },
        { label: "Code Review Limit 5d21h" },
        { label: "Code Review Secondary 8d03h" },
      ]}
    >
      <Gauge value={30} label="Usage 5h" suffix="30.0%" />
      <Gauge value={58} label="Usage 7d" suffix="58.0%" />
      <div className="text-[#7a829e] text-[10px] mt-0.5">421k usage used</div>
      <div className="text-[#7a829e] text-[10px]">221.4k / 258.4k tokens</div>

      <Section title="Model Burn (tokens)" />
      <ModelRow rank={1} name="gpt-5-1-codex-max" pct={32} metric="32% 13.6M tok" />
      <ModelRow rank={2} name="gpt-5-2-codex" pct={26} metric="26% 11.2M tok" />
      <ModelRow rank={3} name="gpt-5-3-codex" pct={21} metric="21% 9.1M tok" />
      <ModelRow rank={4} name="gpt-5-1-codex-mini" pct={21} metric="21% 9.1M tok" />
      <div className="text-[10px] text-[#2a3045] ml-[25px]">Trend (daily by model)</div>
      <div className="text-[10px] text-[#2a3045] ml-[25px]">gpt-5-1-cod...</div>
      <div className="text-[10px] text-[#2a3045] ml-[25px]">gpt-5-2-cod...</div>

      <Section title="Clients" />
      <ModelRow rank={1} name="CLI Agents" pct={41} metric="41% 5.3k req" />
      <ModelRow rank={2} name="Cloud Agents" pct={26} metric="26% 3.4k req" />
      <ModelRow rank={3} name="Desktop App" pct={25} metric="25% 3.3k req" />
      <ModelRow rank={4} name="IDE" pct={8} metric="8% 1.1k req" />
      <div className="text-[10px] text-[#2a3045] ml-[25px]">Trend (daily by client)</div>
      <div className="text-[10px] text-[#2a3045] ml-[25px]">CLI Agents</div>
      <div className="text-[10px] text-[#2a3045] ml-[25px]">Cloud Agents</div>

      <Section title="Tool Usage" />
      <div className="text-[#7a829e] text-[10px] -mt-0.5 mb-0.5 ml-[25px]">63.1k calls · 27% ok</div>
      <ModelRow rank={1} name="click" pct={9} metric="9% 5.8k" />
      <ModelRow rank={2} name="gofmt" pct={9} metric="9% 5.5k" />
      <ModelRow rank={3} name="mcp_kubernetes" pct={9} metric="9% 5.5k" />
      <ModelRow rank={4} name="update_plans" pct={8} metric="8% 5.1k" />
      <ModelRow rank={5} name="go_test" pct={8} metric="8% 4.8k" />
      <ModelRow rank={6} name="web_search" pct={7} metric="7% 4.7k" />
      <div className="text-[10px] text-[#2a3045] ml-[25px]">› 10 more tools (Ctrl+0)</div>

      <Section title="Language (requests)" />
      <ModelRow rank={1} name="ts" pct={20} metric="20% 5.5k req" />
      <ModelRow rank={2} name="md" pct={18} metric="18% 4.9k req" />
      <ModelRow rank={3} name="go" pct={16} metric="16% 4.4k req" />
      <ModelRow rank={4} name="python" pct={16} metric="16% 4.2k req" />
      <ModelRow rank={5} name="json" pct={13} metric="13% 3.4k req" />
      <ModelRow rank={6} name="yaml" pct={9} metric="9% 2.6k req" />
      <div className="text-[10px] text-[#2a3045] ml-[25px]">› 2 more languages (Ctrl+0)</div>

      <Section title="Code Statistics" />
      <CodeStats
        added={63}
        removed={12}
        filesChanged="1.2k files"
        commits="1.6k commits · 35% AI"
        prompts="3.8k total"
      />

      <div className="mt-2 pt-2 border-t border-[#1e2130] space-y-0.5">
        <StatLine label="Credits" value="balance $298.51" />
        <StatLine label="Usage" value="used 74% · auto 14% · op 14% · ctx 39%" />
        <StatLine label="Activity" value="today 4.3k · all 3.7k · sess 3.9k sessions · reqs 5.9k" />
        <StatLine label="Tokens" value="today tok 5.3M · 7d tok 5.2M · 30d 917.8k" />
        <StatLine label="Lines" value="added 63 · removed 12 · commits 1.6k commits · prompts 3.8k prompts" />
      </div>

      <div className="mt-1.5 space-y-0.5">
        <StatLine label="AI Deleted" value="3811 files" />
        <StatLine label="AI Tracked" value="258.4k tok" />
        <StatLine label="Context Window" value="221.4k / 258.4k tok" />
        <StatLine label="Session Total Tokens" value="154.1k tok" />
        <StatLine label="Today Composer Requests" value="5784 req" />
      </div>

      <div className="mt-1.5 text-[#7a829e] text-[10px] italic">Codex live usage + local session data</div>

      <div className="mt-2 pt-2 border-t border-[#1e2130] space-y-0.5 text-[10px]">
        <StatLine label="Account" value="anon.codex.user@example.invalid" />
        <StatLine label="Type" value="team" />
        <StatLine label="Version" value="0.105.0" />
      </div>
    </Panel>
  );
}
