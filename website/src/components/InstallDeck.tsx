import { useCallback, useMemo, useState } from "react";
import AnimatedSection from "./ui/AnimatedSection";

type InstallTarget = "mac_linux" | "windows";

const installCommands: Record<InstallTarget, string> = {
  mac_linux: "brew install janekbaraniewski/tap/openusage",
  windows:
    "curl -fsSL https://github.com/janekbaraniewski/openusage/releases/latest/download/install.sh | bash",
};

function detectOS(): InstallTarget {
  if (typeof navigator === "undefined") {
    return "mac_linux";
  }

  return navigator.userAgent.toLowerCase().includes("win") ? "windows" : "mac_linux";
}

export default function InstallDeck() {
  const defaultTarget = useMemo(detectOS, []);
  const [target, setTarget] = useState<InstallTarget>(defaultTarget);
  const [copied, setCopied] = useState(false);

  const command = installCommands[target];

  const handleCopy = useCallback(() => {
    navigator.clipboard
      .writeText(command)
      .then(() => {
        setCopied(true);
        window.setTimeout(() => setCopied(false), 1300);
      })
      .catch(() => undefined);
  }, [command]);

  return (
    <section className="section section-install" id="install">
      <div className="container install-layout">
        <AnimatedSection className="section-head">
          <p className="section-label">Install Command Center</p>
          <h2>One command to launch full visibility.</h2>
          <p>
            Keep data local. Keep ownership local. Integrate with the tools your team already uses.
          </p>
        </AnimatedSection>

        <AnimatedSection delay={0.08}>
          <div className="install-shell">
            <div className="install-tabs" role="tablist" aria-label="Choose operating system">
              <button
                type="button"
                role="tab"
                aria-selected={target === "mac_linux"}
                className={target === "mac_linux" ? "is-active" : ""}
                onClick={() => setTarget("mac_linux")}
              >
                macOS / Linux
              </button>
              <button
                type="button"
                role="tab"
                aria-selected={target === "windows"}
                className={target === "windows" ? "is-active" : ""}
                onClick={() => setTarget("windows")}
              >
                Windows
              </button>
            </div>

            <div className="install-command">
              <code>{command}</code>
              <button type="button" className="button button-ghost" onClick={handleCopy}>
                {copied ? "Copied" : "Copy"}
              </button>
            </div>

            <p className="install-note">
              No hosted collector. No telemetry lock-in. Your usage stream stays on your machine.
            </p>
          </div>
        </AnimatedSection>
      </div>
    </section>
  );
}
