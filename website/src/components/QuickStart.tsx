import { useMemo, useState } from "react";
import AnimatedSection from "./ui/AnimatedSection";
import CodeBlock from "./ui/CodeBlock";

function detectOS(): "mac" | "other" {
  if (typeof navigator === "undefined") return "other";
  return navigator.userAgent.toLowerCase().includes("mac") ? "mac" : "other";
}

const installMethods = {
  mac: "brew install janekbaraniewski/tap/openusage",
  other:
    "curl -fsSL https://github.com/janekbaraniewski/openusage/releases/latest/download/install.sh | bash",
};

export default function QuickStart() {
  const os = useMemo(detectOS, []);
  const [method, setMethod] = useState<"mac" | "other">(os);

  return (
    <section id="install" className="py-16 sm:py-20">
      <div className="mx-auto max-w-4xl px-6">
        <AnimatedSection>
          <div className="shell-card rounded-2xl p-6 sm:p-7">
            <div className="mb-4 flex flex-wrap gap-2">
              <button
                onClick={() => setMethod("mac")}
                className={`rounded-full px-3.5 py-1.5 font-mono text-[11px] uppercase tracking-[0.1em] transition-colors ${
                  method === "mac"
                    ? "bg-teal text-ink"
                    : "border border-line text-muted hover:border-teal hover:text-teal"
                }`}
              >
                macOS
              </button>
              <button
                onClick={() => setMethod("other")}
                className={`rounded-full px-3.5 py-1.5 font-mono text-[11px] uppercase tracking-[0.1em] transition-colors ${
                  method === "other"
                    ? "bg-teal text-ink"
                    : "border border-line text-muted hover:border-teal hover:text-teal"
                }`}
              >
                Linux / Other
              </button>
            </div>

            <CodeBlock code={installMethods[method]} />
          </div>
        </AnimatedSection>
      </div>
    </section>
  );
}
