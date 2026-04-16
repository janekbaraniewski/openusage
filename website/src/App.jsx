import { useEffect, useRef, useState } from "react";
import {
  acceptAnalytics,
  analyticsConfigured,
  declineAnalytics,
  hasConsentChoice,
  track,
} from "./analytics";

const base = import.meta.env.BASE_URL;

/* ────────────────────────────────────────────────────────────────
   Scroll reveal
   ──────────────────────────────────────────────────────────────── */

function useReveal(threshold = 0.12) {
  const ref = useRef(null);
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const obs = new IntersectionObserver(
      ([e]) => { if (e.isIntersecting) { el.classList.add("v"); obs.unobserve(el); } },
      { threshold },
    );
    obs.observe(el);
    return () => obs.disconnect();
  }, [threshold]);
  return ref;
}

function R({ children, delay = 0, className = "" }) {
  const ref = useReveal();
  return (
    <div ref={ref} className={`r ${className}`} style={delay ? { transitionDelay: `${delay}s` } : undefined}>
      {children}
    </div>
  );
}

/* Lazy video — only loads sources when scrolled into view */
function LazyVideo({ sources, className, style, startAt, onCanPlay, ...props }) {
  const ref = useRef(null);
  const [loaded, setLoaded] = useState(false);
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const obs = new IntersectionObserver(
      ([e]) => { if (e.isIntersecting) { setLoaded(true); obs.unobserve(el); } },
      { rootMargin: "200px" },
    );
    obs.observe(el);
    return () => obs.disconnect();
  }, []);
  return (
    <video
      ref={ref}
      className={className}
      style={style}
      autoPlay={loaded}
      loop
      muted
      playsInline
      preload="none"
      onLoadedMetadata={(e) => { if (startAt && e.currentTarget.duration > startAt) e.currentTarget.currentTime = startAt; }}
      onCanPlay={(e) => { e.currentTarget.play().catch(() => {}); }}
      {...props}
    >
      {loaded && sources.map((s) => <source key={s.src} src={s.src} type={s.type} />)}
    </video>
  );
}

/* ────────────────────────────────────────────────────────────────
   Banner — exact TUI characters, gradient per-column
   ──────────────────────────────────────────────────────────────── */

const bannerLines = [
  " █▀█ █▀█ █▀▀ █▄░█   █░█ █▀ ▄▀█ █▀▀ █▀▀",
  " █▄█ █▀▀ ██▄ █░▀█   █▄█ ▄█ █▀█ █▄█ ██▄",
];

const gradient = ["#b8bb26", "#83a598", "#4EC5C1", "#d3869b", "#b16286", "#fabd2f"];

/* Shared shift for all Banner instances to stay in sync */
let globalShift = 0;
setInterval(() => { globalShift++; }, 450);

function useShift() {
  const [s, set] = useState(globalShift);
  useEffect(() => {
    const id = setInterval(() => set(globalShift), 450);
    return () => clearInterval(id);
  }, []);
  return s;
}

function Banner({ className, lines = bannerLines }) {
  const shift = useShift();
  return (
    <pre className={className} aria-label="OpenUsage" role="img">
      {lines.map((line, li) => (
        <div key={li} aria-hidden="true">
          {[...line].map((ch, i) =>
            ch === " " ? <span key={i}>{" "}</span>
              : <span key={i} style={{ color: gradient[Math.floor(i / 2 + shift) % gradient.length] }}>{ch}</span>
          )}
        </div>
      ))}
    </pre>
  );
}

function NavLogo() {
  return (
    <div className="nav__logo-wrap" aria-label="OpenUsage">
      <Banner className="banner nav__logo-inner" />
    </div>
  );
}

/* ────────────────────────────────────────────────────────────────
   Provider data — from README provider tables
   ──────────────────────────────────────────────────────────────── */

const icon = (name) => `${base}icons/${name}.svg`;

const codingAgents = [
  { name: "Claude Code",    icon: icon("claudecode") },
  { name: "Cursor",         icon: icon("cursor") },
  { name: "GitHub Copilot", icon: icon("copilot") },
  { name: "Codex CLI",      icon: icon("codex") },
  { name: "Gemini CLI",     icon: icon("geminicli") },
  { name: "OpenCode",       icon: icon("opencode") },
  { name: "Ollama",         icon: icon("ollama") },
];

const apiPlatforms = [
  { name: "OpenAI",            icon: icon("openai") },
  { name: "Anthropic",         icon: icon("anthropic") },
  { name: "OpenRouter",        icon: icon("openrouter") },
  { name: "Groq",              icon: icon("groq") },
  { name: "Mistral AI",        icon: icon("mistral") },
  { name: "DeepSeek",          icon: icon("deepseek") },
  { name: "xAI",               icon: icon("xai") },
  { name: "Z.AI",              icon: icon("zai") },
  { name: "Google Gemini API", icon: icon("gemini") },
  { name: "Alibaba Cloud",    icon: icon("alibabacloud") },
];

const installData = [
  { label: "Brew",   cmd: "brew install janekbaraniewski/tap/openusage" },
  { label: "Script", cmd: "curl -fsSL https://github.com/janekbaraniewski/openusage/releases/latest/download/install.sh | bash" },
  { label: "Go",     cmd: "go install github.com/janekbaraniewski/openusage/cmd/openusage@latest" },
];

/* ────────────────────────────────────────────────────────────────
   App
   ──────────────────────────────────────────────────────────────── */

export default function App() {
  const [copied, setCopied] = useState("");
  const [scrolled, setScrolled] = useState(false);
  const [showConsentBanner, setShowConsentBanner] = useState(false);

  useEffect(() => {
    const onScroll = () => setScrolled(window.scrollY > 100);
    onScroll();
    window.addEventListener("scroll", onScroll, { passive: true });
    return () => window.removeEventListener("scroll", onScroll);
  }, []);

  useEffect(() => {
    setShowConsentBanner(analyticsConfigured() && !hasConsentChoice());
  }, []);

  useEffect(() => {
    if (!copied) return;
    const t = setTimeout(() => setCopied(""), 1500);
    return () => clearTimeout(t);
  }, [copied]);

  async function copy(cmd) {
    try {
      await navigator.clipboard.writeText(cmd);
      setCopied(cmd);
      track("install command copied", { command: cmd });
    }
    catch { setCopied(""); }
  }

  function trackCTA(location, target) {
    track("cta clicked", { location, target });
  }

  function trackOutbound(target, location) {
    track("outbound link clicked", { location, target });
  }

  function acceptTracking() {
    acceptAnalytics();
    setShowConsentBanner(false);
  }

  function declineTracking() {
    declineAnalytics();
    setShowConsentBanner(false);
  }

  return (
    <>
      {/* ── Nav ──────────────────────────────────────── */}
      <nav className={`nav${scrolled ? " nav--visible" : ""}`}>
        <NavLogo />
        <div className="nav__right">
          <a className="nav__link" href="#providers">Providers</a>
          <a className="nav__link" href="https://github.com/janekbaraniewski/openusage" rel="noreferrer" target="_blank" onClick={() => trackOutbound("github", "nav")}>GitHub</a>
          <a className="nav__cta" href="#install" onClick={() => trackCTA("nav", "install")}>Install</a>
        </div>
      </nav>

      {/* ── Hero (left-aligned) ──────────────────────── */}
      <main>
      <section className="hero">
        <div className="w" style={{ textAlign: 'left' }}>
          <R><Banner className="banner" /></R>
          <R delay={0.15}>
            <h1 className="hero__title">
              Track AI coding usage, spend, and quotas from your terminal.
            </h1>
          </R>
          <R delay={0.25}>
            <p className="hero__sub">
              OpenUsage auto-detects supported tools and common API key env vars,
              then shows spend, quotas, rate limits, model usage, and session telemetry
              in one keyboard-first dashboard.
            </p>
          </R>
          <R delay={0.35}>
            <div className="hero__actions">
              <a className="btn btn--fill" href="#install" onClick={() => trackCTA("hero", "install")}>Get started</a>
              <a className="btn btn--ghost" href="https://github.com/janekbaraniewski/openusage" rel="noreferrer" target="_blank" onClick={() => trackOutbound("github", "hero")}>GitHub →</a>
            </div>
          </R>
        </div>
      </section>

      {/* ── Pitch (alternating alignment) ────────────── */}
      <section className="pitch">
        <div className="w">
          <R as="p" className="pitch__line">
            <em>Auto-detects</em> your coding tools and common API key env vars.
          </R>
          <R className="pitch__line" delay={0.12}>
            <p className="pitch__line" style={{margin:0}}>
              Shows <em>spend, quotas,</em> and <em>model usage</em> in one place.
            </p>
          </R>
          <R className="pitch__line" delay={0.24}>
            <p className="pitch__line" style={{margin:0}}>
              Runs locally. Install it, then run <code>openusage</code>.
            </p>
          </R>
        </div>
      </section>

      {/* ── Demo — dashboard views ────────────────────── */}
      <section className="demo" id="demo">
        <div className="w">
          <R>
            <div className="demo__frame">
              <LazyVideo sources={[
                { src: `${base}media/dash-views.webm`, type: "video/webm" },
                { src: `${base}media/dash-views.mp4`, type: "video/mp4" },
              ]} />
            </div>
          </R>
          <R><p className="demo__caption">dashboard · detail · compare · analytics views</p></R>
        </div>
      </section>

      {/* ── Providers (asymmetric: title left, grid below) ── */}
      <section className="prov-section" id="providers">
        <div className="w">
          <R>
            <div className="prov-header">
              <h2 className="prov-header__title">17 providers</h2>
              <p className="prov-header__sub">
                Coding agents, API platforms, and local runtimes.<br />One place to watch them all.
              </p>
            </div>
          </R>

          <div className="prov-grid">
            <div className="prov-col">
              <R><div className="prov-col__label prov-col__label--agents">Coding Agents &amp; IDEs</div></R>
              {codingAgents.map((p, i) => (
                <R key={p.name} delay={i * 0.04}>
                  <div className="prov-item">
                    <img className="prov-logo" src={p.icon} alt="" aria-hidden="true" loading="lazy" />
                    <span className="prov-name">{p.name}</span>
                  </div>
                </R>
              ))}
            </div>

            <div className="prov-col">
              <R><div className="prov-col__label prov-col__label--api">API Platforms</div></R>
              {apiPlatforms.map((p, i) => (
                <R key={p.name} delay={i * 0.03}>
                  <div className="prov-item">
                    <img className="prov-logo" src={p.icon} alt="" aria-hidden="true" loading="lazy" />
                    <span className="prov-name">{p.name}</span>
                  </div>
                </R>
              ))}
            </div>
          </div>
        </div>
      </section>

      {/* ── Side-by-side video ────────────────────────────── */}
      <section className="demo">
        <div className="w">
          <R>
            <p className="demo__caption" style={{ textAlign: 'left', marginBottom: 16, fontSize: '1rem', color: 'var(--text-2)' }}>
              Keep it open beside the agent you are using.
            </p>
          </R>
          <R>
            <div className="demo__frame">
              <LazyVideo
                startAt={2.6}
                sources={[
                  { src: `${base}media/openusage-openrouter-opencode-fast.webm`, type: "video/webm" },
                  { src: `${base}media/openusage-openrouter-opencode-fast.mp4`, type: "video/mp4" },
                ]}
              />
            </div>
          </R>
          <R><p className="demo__caption">OpenUsage running alongside OpenCode monitoring live OpenRouter usage.</p></R>
        </div>
      </section>

      {/* ── Features (keyword-rich, 2-col grid) ─────────── */}
      <section className="features-section" id="features">
        <div className="w">
          <R><h2 className="features-title">What it shows</h2></R>
          <R delay={0.05}>
            <p className="features-lede">
              The useful stuff: spend, quotas, model activity, session history, and tool telemetry.
              No vanity counters. No decorative dashboard filler.
            </p>
          </R>
          <div className="features-grid">
            <R><div className="feature-item">
              <h3>Spend, credits, and quotas</h3>
              <p>See daily spend, remaining credits, plan usage, resets, and burn rate wherever the provider exposes them.</p>
            </div></R>
            <R delay={0.06}><div className="feature-item">
              <h3>Token and model breakdowns</h3>
              <p>Compare input, output, cached, and reasoning tokens with per-model usage where provider or local telemetry supports it.</p>
            </div></R>
            <R delay={0.12}><div className="feature-item">
              <h3>Session and client activity</h3>
              <p>Inspect sessions, projects, clients, and daily trends from supported local telemetry and provider APIs.</p>
            </div></R>
            <R delay={0.18}><div className="feature-item">
              <h3>MCP and tool usage</h3>
              <p>See which MCP servers and tools were used, how often they ran, and which sessions they appeared in for supported integrations.</p>
            </div></R>
            <R delay={0.24}><div className="feature-item">
              <h3>Daemon-backed history</h3>
              <p>Keep collecting data in the background and analyze time windows from the local SQLite telemetry store.</p>
            </div></R>
            <R delay={0.30}><div className="feature-item">
              <h3>Code statistics</h3>
              <p>Track lines added, files touched, and language mix from supported coding-tool telemetry.</p>
            </div></R>
          </div>
        </div>
      </section>

      {/* ── Settings video ───────────────────────────────── */}
      <section className="demo">
        <div className="w">
          <R>
            <p className="demo__caption" style={{ textAlign: 'left', marginBottom: 16, fontSize: '1rem', color: 'var(--text-2)' }}>
              Rearrange dashboard sections. Tune detail graphs. Switch time windows. Set thresholds.
            </p>
          </R>
          <R>
            <div className="demo__frame" style={{ aspectRatio: '16 / 8.56' }}>
              <LazyVideo
                style={{ objectFit: 'cover', objectPosition: 'center 48.5%' }}
                sources={[
                  { src: `${base}media/tile-config-example.webm`, type: "video/webm" },
                  { src: `${base}media/tile-config-example.mp4`, type: "video/mp4" },
                ]}
              />
            </div>
          </R>
          <R><p className="demo__caption">Settings modal — layout, graphs, thresholds, and live preview</p></R>
        </div>
      </section>

      {/* ── Install (left-heavy grid) ────────────────── */}
      <section className="install-section" id="install">
        <div className="w">
          <R>
            <div className="install-header">
              <h2 className="install-title">Get started</h2>
              <p className="install-desc">
                Install it, run <code>openusage</code>, and let auto-detection pick up supported
                tools and common API key env vars. Add manual accounts later only if you need to.
              </p>
            </div>
          </R>

          <div className="install-cmds">
            {installData.map((item, i) => (
              <R key={item.label} delay={i * 0.06}>
                <div className="install-row">
                  <span className="install-label">{item.label}</span>
                  <code className="install-code">{item.cmd}</code>
                  <button
                    className={`install-copy${copied === item.cmd ? " install-copy--ok" : ""}`}
                    onClick={() => copy(item.cmd)}
                    type="button"
                  >{copied === item.cmd ? "Copied" : "Copy"}</button>
                </div>
              </R>
            ))}
          </div>

          <R delay={0.2}>
            <p className="install-run">Then just run <code>openusage</code></p>
          </R>
        </div>
      </section>

      </main>

      {/* ── Footer ───────────────────────────────────── */}
      <footer className="footer">
        <div className="w" style={{ display: "flex", justifyContent: "space-between", alignItems: "center", width: "100%" }}>
          <span>OpenUsage · open source</span>
          <div className="footer__links">
            <a className="footer__link" href="https://github.com/janekbaraniewski/openusage" rel="noreferrer" target="_blank" onClick={() => trackOutbound("github", "footer")}>GitHub</a>
            <a className="footer__link" href="https://github.com/janekbaraniewski/openusage/releases" rel="noreferrer" target="_blank" onClick={() => trackOutbound("releases", "footer")}>Releases</a>
          </div>
        </div>
      </footer>
      {showConsentBanner ? (
        <div className="consent-banner" role="dialog" aria-live="polite" aria-label="Analytics preference">
          <p className="consent-banner__text">
            Allow privacy-respecting analytics so we can see pageviews, GitHub clicks, and install intent.
          </p>
          <div className="consent-banner__actions">
            <button className="consent-banner__button consent-banner__button--primary" type="button" onClick={acceptTracking}>
              Allow
            </button>
            <button className="consent-banner__button" type="button" onClick={declineTracking}>
              Decline
            </button>
          </div>
        </div>
      ) : null}
    </>
  );
}
