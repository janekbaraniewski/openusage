import Panel from "../Panel";
import Gauge from "../Gauge";
import Section from "../Section";
import ModelRow from "../ModelRow";
import StatLine from "../StatLine";

interface ViewProps {
  bodyMinWidth?: number;
}

export default function OpenRouterView({ bodyMinWidth = 580 }: ViewProps) {
  return (
    <Panel
      name="openrouter"
      status="OK"
      bodyMinWidth={bodyMinWidth}
      tabs={[{ label: "Credits", active: true }]}
    >
      <Gauge value={78.5} label="Credit Balance" suffix="78.5%" />
      <div className="text-[#7a829e] text-[10px] mt-0.5">$7.85 / $10.00 spent</div>
      <div className="text-[#7a829e] text-[10px]">$2.15 remaining · today $14.71 · week $105.78 · 24 models</div>

      <Section title="Model Burn (credits)" />
      <ModelRow rank={1} name="moonshotai-kimi-k2-5" pct={21} metric="21% 15.9M tok · $462.25" />
      <ModelRow rank={2} name="deepseek-deepseek-v3-2" pct={31} metric="31% 12.4M tok · $675.30" />
      <ModelRow rank={3} name="qwen3-coder-flash" pct={17} metric="17% 11.4M tok · $360.74" />
      <ModelRow rank={4} name="nvidia-nemotron-nano-9b-v2" pct={5} metric="5% 5.5M tok · $103.76" />
      <ModelRow rank={5} name="openai-gpt-o4s-r1203" pct={26} metric="26% 3.8M tok · $560.99" />

      <Section title="Projects" />
      <ModelRow rank={1} name="Recipe Blog" pct={34} metric="34% 8.8M tok · 2.3k req" />
      <ModelRow rank={2} name="Workout Log" pct={24} metric="24% 6.3M tok · 1.7k req" />
      <ModelRow rank={3} name="Sandbox" pct={18} metric="18% 4.7M tok · 4.5k req" />
      <ModelRow rank={4} name="Expense Tracker" pct={17} metric="17% 4.3M tok · 2.2k req" />
      <ModelRow rank={5} name="Pet Tracker" pct={4} metric="4% 954.5k tok · 458 req" />
      <ModelRow rank={6} name="Garden Planner" pct={2} metric="2% 618.7k tok · 3.1k req" />
      <div className="flex gap-px items-end h-3 my-1 ml-[25px]">
        {[
          { h: 80, c: "#ff90b2" },
          { h: 55, c: "#f0a870" },
          { h: 45, c: "#f2d28d" },
          { h: 40, c: "#9de9bd" },
          { h: 15, c: "#7be3d6" },
          { h: 10, c: "#c8a8ff" },
        ].map((d, i) => (
          <div key={i} className="w-[5px]" style={{ height: `${d.h}%`, backgroundColor: d.c }} />
        ))}
      </div>
      <div className="text-[10px] text-[#2a3045] ml-[25px]">Recipe Blog · Workout Log · Sandbox · Expense Tra...</div>

      <Section title="Hosting Providers (credits)" />
      <ModelRow rank={1} name="novita" pct={28} metric="28% 14.4M tok · 1.2k req · $676.69" />
      <ModelRow rank={2} name="google-vertex" pct={15} metric="15% 14.3M tok · 5.1k req · $353.46" />
      <ModelRow rank={3} name="alibaba" pct={14} metric="14% 13.8M tok · 1.4k req · $333.63" />
      <ModelRow rank={4} name="together" pct={10} metric="10% 13.0M tok · 481 req · $236.66" />
      <ModelRow rank={5} name="deepinfra" pct={5} metric="5% 12.6M tok · 5.5k req · $120.03" />
      <ModelRow rank={6} name="fireworks" pct={10} metric="10% 12.2M tok · 5.3k req · $241.76" />
      <ModelRow rank={7} name="siliconflow-int4" pct={10} metric="10% 8.3M tok · 682 req · $457.53" />

      <Section title="Tool Usage" />
      <ModelRow rank={1} name="qwen_qwen3-coder-flash" pct={37} metric="37% 3.8k" />
      <ModelRow rank={2} name="deepseek-deepseek-v3-2" pct={26} metric="26% 4.1k" />
      <ModelRow rank={3} name="nvidia_nemotron-nano-9b-v2" pct={19} metric="19% 3.0k" />
      <ModelRow rank={4} name="moonshotai_kimi-k2-5" pct={13} metric="13% 2.0k" />
      <ModelRow rank={5} name="openai_gpt-o4s-r1203" pct={5} metric="5% 822" />

      <Section title="Language (requests)" />
      <ModelRow rank={1} name="general" pct={40} metric="40% 4.7k req" />
      <ModelRow rank={2} name="multimodal" pct={33} metric="33% 3.9k req" />
      <ModelRow rank={3} name="reasoning" pct={17} metric="17% 2.0k req" />
      <ModelRow rank={4} name="code" pct={11} metric="11% 1.3k req" />

      <Section title="Daily Usage" />
      <div className="flex gap-0.5 items-end h-4 mt-1 ml-[25px]">
        <div className="flex flex-col gap-px">
          <div className="w-2 h-1" style={{ backgroundColor: "#9de9bd" }} />
          <div className="w-2 h-1" style={{ backgroundColor: "#f2d28d" }} />
          <div className="w-2 h-1" style={{ backgroundColor: "#f0a870" }} />
        </div>
        <div className="text-[9px] text-[#2a3045] ml-1">Cost · Req · Tokens</div>
      </div>
      <div className="flex gap-px items-end h-5 mt-1 ml-[25px]">
        {[3, 5, 7, 4, 8, 6, 2].map((v, i) => (
          <div
            key={i}
            className="w-2"
            style={{
              height: `${(v / 8) * 100}%`,
              backgroundColor: i < 3 ? "#9de9bd" : i < 5 ? "#f2d28d" : "#f0a870",
            }}
          />
        ))}
      </div>

      <div className="mt-2 pt-2 border-t border-[#1e2130] space-y-0.5">
        <StatLine label="Credits" value="balance $7.85/$10.00 · $215.71 · $48.12 · $474.85 · $378.12" />
        <StatLine label="Spent" value="today $14.71 · 7d $105.78 · 30d $244.52" />
        <StatLine label="Activity" value="3.7k · ana 7d req 2.4k · ana 30d req 5.5k · records 300 · 15.1k keys" />
        <StatLine label="Tokens" value="3.4M · 8.3M · 3.1M · 2.4M · ana 7d tok 8.8M" />
        <StatLine label="Per" value="$39.72/h" />
      </div>

      <div className="mt-1.5 text-[#7a829e] text-[10px]">$2.15 credits remaining</div>

      <div className="mt-2 pt-2 border-t border-[#1e2130] space-y-0.5 text-[10px]">
        <StatLine label="Key" value="demo-key" />
        <StatLine label="Tier" value="premium" />
      </div>
    </Panel>
  );
}
