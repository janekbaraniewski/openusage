import { useCallback, useMemo, useState } from "react";

type InstallTarget = "unix" | "windows";

const installMethods: Record<InstallTarget, string> = {
  unix: "brew install janekbaraniewski/tap/openusage",
  windows:
    "curl -fsSL https://github.com/janekbaraniewski/openusage/releases/latest/download/install.sh | bash",
};

function detectOS(): InstallTarget {
  if (typeof navigator === "undefined") return "unix";
  return navigator.userAgent.toLowerCase().includes("win") ? "windows" : "unix";
}

export default function Hero() {
  const detected = useMemo(detectOS, []);
  const [selectedTarget, setSelectedTarget] = useState<InstallTarget>(detected);
  const installCmd = installMethods[selectedTarget];
  const [copied, setCopied] = useState(false);

  const handleCopy = useCallback(() => {
    navigator.clipboard
      .writeText(installCmd)
      .then(() => {
        setCopied(true);
        setTimeout(() => setCopied(false), 1200);
      })
      .catch(() => undefined);
  }, [installCmd]);

  return (
    <section className="hero" id="install">
      <div className="container">
        <div className="hero-grid">
          <div className="hero-copy">
            <p className="eyebrow">
              <span className="eyebrow-dot" aria-hidden="true" />
              OpenUsage / Local telemetry runtime
            </p>
            <h1>Know what your agents cost.</h1>
            <p className="subtitle">
              Open source terminal dashboard for usage and spend across Codex,
              Cursor, Claude Code, Copilot, OpenCode, and more.
            </p>

            <div className="hero-actions">
              <a href="#install-cmd" className="button button-solid">
                Install
              </a>
              <a
                href="https://github.com/janekbaraniewski/openusage"
                target="_blank"
                rel="noopener noreferrer"
                className="button button-ghost"
              >
                GitHub
              </a>
            </div>

            <div className="install-card" id="install-cmd">
              <div className="install-tabs" role="tablist" aria-label="Operating system">
                <button
                  type="button"
                  role="tab"
                  aria-selected={selectedTarget === "unix"}
                  className={`install-tab ${selectedTarget === "unix" ? "is-active" : ""}`}
                  onClick={() => setSelectedTarget("unix")}
                >
                  macOS / Linux
                </button>
                <button
                  type="button"
                  role="tab"
                  aria-selected={selectedTarget === "windows"}
                  className={`install-tab ${selectedTarget === "windows" ? "is-active" : ""}`}
                  onClick={() => setSelectedTarget("windows")}
                >
                  Windows
                </button>
              </div>
              <div className="install-row">
                <code>{installCmd}</code>
                <button type="button" onClick={handleCopy} className="button button-ghost">
                  {copied ? "Copied" : "Copy"}
                </button>
              </div>
            </div>

            <p className="hero-note">
              No cloud account. No telemetry lock-in. Just local data.
            </p>
          </div>

          <div className="preview-shell" aria-label="Terminal dashboard preview">
            <div className="preview-head" data-label="proof">
              <span>openusage dashboard</span>
              <span className="status-dot" aria-hidden="true" />
            </div>
            <img
              src={`${import.meta.env.BASE_URL}assets/dashboard.png`}
              alt="OpenUsage dashboard preview"
            />
          </div>
        </div>
      </div>
    </section>
  );
}
