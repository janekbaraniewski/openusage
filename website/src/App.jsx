import { useEffect, useRef, useState } from "react";
import {
  acceptAnalytics,
  analyticsConsentChoice,
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

// Coding agents, CLIs, and IDE tools OpenUsage tracks locally. Providers
// without a bundled logo (icon: null) render a monogram tile. Keep this list in
// sync with internal/providers AllProviders() and the provider count used
// across the page, JSON-LD, and llms.txt.
const codingAgents = [
  { name: "Claude Code",    icon: icon("claudecode") },
  { name: "Codex CLI",      icon: icon("codex") },
  { name: "Cursor",         icon: icon("cursor") },
  { name: "GitHub Copilot", icon: icon("copilot") },
  { name: "Gemini CLI",     icon: icon("geminicli") },
  { name: "OpenCode",       icon: icon("opencode") },
  { name: "Ollama",         icon: icon("ollama") },
  { name: "Amp",            icon: icon("amp") },
  { name: "Goose",          icon: icon("goose") },
  { name: "Hermes",         icon: icon("hermes") },
  { name: "Kilo Code",      icon: icon("kilocode") },
  { name: "Kimi CLI",       icon: icon("kimi") },
  { name: "Kiro",           icon: icon("kiro") },
  { name: "Roo Code",       icon: icon("roocode") },
  { name: "Zed",            icon: icon("zed") },
  { name: "Qwen CLI",       icon: icon("qwen") },
  { name: "OpenClaw",       icon: icon("openclaw") },
  { name: "Crush",          icon: null },
  { name: "Droid",          icon: null },
  { name: "Codebuff",       icon: null },
  { name: "Mux",            icon: null },
  { name: "Pi",             icon: null },
];

const apiPlatforms = [
  { name: "OpenAI",            icon: icon("openai") },
  { name: "Anthropic",         icon: icon("anthropic") },
  { name: "OpenRouter",        icon: icon("openrouter") },
  { name: "Groq",              icon: icon("groq") },
  { name: "Mistral AI",        icon: icon("mistral") },
  { name: "DeepSeek",          icon: icon("deepseek") },
  { name: "Moonshot",          icon: icon("moonshot") },
  { name: "Perplexity",        icon: icon("perplexity") },
  { name: "xAI",               icon: icon("xai") },
  { name: "Z.AI",              icon: icon("zai") },
  { name: "Google Gemini API", icon: icon("gemini") },
  { name: "Alibaba Cloud",     icon: icon("alibabacloud") },
];

// Total provider count, derived so copy/headings never drift from the lists.
const providerCount = codingAgents.length + apiPlatforms.length;

const installData = [
  { label: "Brew",   cmd: "brew install janekbaraniewski/tap/openusage" },
  { label: "Script", cmd: "curl -fsSL https://github.com/janekbaraniewski/openusage/releases/latest/download/install.sh | bash" },
  { label: "Go",     cmd: "go install github.com/janekbaraniewski/openusage/cmd/openusage@latest" },
];

const resourceCards = [
  {
    id: "comparison",
    eyebrow: "Comparison",
    title: "OpenUsage.sh vs OpenUsage.ai",
    description: "What's the difference. Short answer: different tools, different jobs.",
    href: "/docs/openusage-sh-vs-openusage-ai/",
  },
  {
    id: "matrix",
    eyebrow: "What it tracks",
    title: "Capability matrix",
    description: "What it actually tracks, provider by provider. No marketing.",
    href: "/docs/capability-matrix/",
  },
  {
    id: "local-quota",
    eyebrow: "Quick guide",
    title: "Local quota tracker",
    description: "The short version, if you found us by searching for \"Claude Code quota tracker\" or similar.",
    href: "/local-quota-tracker-for-claude-code-codex-cursor/",
  },
  {
    id: "query-guides",
    eyebrow: "Docs",
    title: "Docs and guides",
    description: "Per-tool guides for Claude Code, Codex, Cursor, OpenRouter, and the rest.",
    href: "/docs/",
  },
];

/* ────────────────────────────────────────────────────────────────
   App
   ──────────────────────────────────────────────────────────────── */

export default function App() {
  const [analyticsAvailable, setAnalyticsAvailable] = useState(false);
  const [copied, setCopied] = useState("");
  const [scrolled, setScrolled] = useState(false);
  const [analyticsChoice, setAnalyticsChoice] = useState(null);
  const [showConsentBanner, setShowConsentBanner] = useState(false);

  useEffect(() => {
    const onScroll = () => setScrolled(window.scrollY > 100);
    onScroll();
    window.addEventListener("scroll", onScroll, { passive: true });
    return () => window.removeEventListener("scroll", onScroll);
  }, []);

  useEffect(() => {
    const configured = analyticsConfigured();
    setAnalyticsAvailable(configured);
    const choice = configured ? analyticsConsentChoice() : null;
    setAnalyticsChoice(choice);
    setShowConsentBanner(configured && !hasConsentChoice());
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
    setAnalyticsChoice("accepted");
    setShowConsentBanner(false);
  }

  function declineTracking() {
    declineAnalytics();
    setAnalyticsChoice("declined");
    setShowConsentBanner(false);
  }

  function openAnalyticsPreferences() {
    if (!analyticsAvailable) {
      return;
    }

    setShowConsentBanner(true);
  }

  function analyticsPreferenceLabel() {
    if (analyticsChoice === "accepted") {
      return "Analytics on";
    }

    if (analyticsChoice === "declined") {
      return "Analytics off";
    }

    return "Analytics";
  }

  return (
    <>
      {/* ── Nav ──────────────────────────────────────── */}
      <nav className={`nav${scrolled ? " nav--visible" : ""}`}>
        <NavLogo />
        <div className="nav__right">
          <a className="nav__link" href="/docs/" onClick={() => trackCTA("nav", "docs")}>Docs</a>
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
            <p className="hero__eyebrow">Open source · Runs locally · {providerCount} providers and counting</p>
          </R>
          <R delay={0.2}>
            <h1 className="hero__title">
              The dashboard your AI tools forgot to build.
            </h1>
          </R>
          <R delay={0.3}>
            <p className="hero__sub">
              Track spend, quotas, and rate limits across the AI coding tools you actually use. Runs locally, in your terminal.
            </p>
          </R>
          <R delay={0.4}>
            <div className="hero__actions">
              <a className="btn btn--fill" href="#install" onClick={() => trackCTA("hero", "install")}>Get started</a>
              <a className="btn btn--ghost" href="/docs/" onClick={() => trackCTA("hero", "docs")}>Docs</a>
            </div>
          </R>
        </div>
      </section>

      {/* ── Pitch (alternating alignment) ────────────── */}
      <section className="pitch">
        <div className="w">
          <R as="p" className="pitch__line">
            <em>Made for</em> people who run more than one coding agent at a time.
          </R>
          <R className="pitch__line" delay={0.12}>
            <p className="pitch__line" style={{margin:0}}>
              <em>Spend, quotas, rate limits, model usage.</em> One screen.
            </p>
          </R>
          <R className="pitch__line" delay={0.24}>
            <p className="pitch__line" style={{margin:0}}>
              Runs locally. Keep it beside the tools you already use.
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
              <h2 className="prov-header__title">{providerCount} providers, and counting</h2>
              <p className="prov-header__sub">
                Coding agents, API platforms, local runtimes. All in one place.
              </p>
            </div>
          </R>

          <div className="prov-grid">
            <div className="prov-col">
              <R><div className="prov-col__label prov-col__label--agents">Coding Agents &amp; IDEs</div></R>
              {codingAgents.map((p, i) => (
                <R key={p.name} delay={i * 0.04}>
                  <div className="prov-item">
                    {p.icon ? (
                      <img className="prov-logo" src={p.icon} alt="" aria-hidden="true" loading="lazy" />
                    ) : (
                      <span className="prov-logo prov-logo--mono" aria-hidden="true">{p.name[0]}</span>
                    )}
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
                    {p.icon ? (
                      <img className="prov-logo" src={p.icon} alt="" aria-hidden="true" loading="lazy" />
                    ) : (
                      <span className="prov-logo prov-logo--mono" aria-hidden="true">{p.name[0]}</span>
                    )}
                    <span className="prov-name">{p.name}</span>
                  </div>
                </R>
              ))}
            </div>
          </div>

          <R delay={0.1}>
            <p className="prov-contribute" style={{ marginTop: '2.5rem', textAlign: 'center', color: 'var(--text-2)', fontSize: '0.95rem' }}>
              Yours missing?{' '}
              <a
                href="https://github.com/janekbaraniewski/openusage/issues/new"
                rel="noreferrer"
                target="_blank"
                onClick={() => trackOutbound("github-issue", "providers")}
                style={{ color: 'inherit', textDecoration: 'underline' }}
              >Open an issue</a>
              {' '}or{' '}
              <a
                href="https://github.com/janekbaraniewski/openusage/pulls"
                rel="noreferrer"
                target="_blank"
                onClick={() => trackOutbound("github-pr", "providers")}
                style={{ color: 'inherit', textDecoration: 'underline' }}
              >send a PR</a>.
            </p>
          </R>
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
          <R><p className="demo__caption">OpenUsage next to OpenCode, watching OpenRouter spend live.</p></R>
        </div>
      </section>

      {/* ── Features (keyword-rich, 2-col grid) ─────────── */}
      <section className="features-section" id="features">
        <div className="w">
          <R><h2 className="features-title">What it does</h2></R>
          <R delay={0.05}>
            <p className="features-lede">
              If you only use one coding agent, you don't need this. If you use two or more, you've probably got five tabs open trying to figure out where your money went. That's the problem we solve.
            </p>
          </R>
          <div className="features-grid">
            <R><div className="feature-item">
              <h3>One place across providers</h3>
              <p>Coding agents, API platforms, and local runtimes side by side. No more cycling through provider dashboards.</p>
            </div></R>
            <R delay={0.06}><div className="feature-item">
              <h3>Quotas, spend, and limits in one view</h3>
              <p>Remaining credits, reset windows, rate limits, and model breakdowns where the provider exposes them.</p>
            </div></R>
            <R delay={0.12}><div className="feature-item">
              <h3>Made for the tools, not the SDK</h3>
              <p>This is for monitoring the AI tools you use to write code. It's not a tracing SDK for the AI app you're building.</p>
            </div></R>
            <R delay={0.18}><div className="feature-item">
              <h3>Local by default</h3>
              <p>No hosted observability plane. The dashboard runs on your machine. History sits in a SQLite file you own.</p>
            </div></R>
            <R delay={0.24}><div className="feature-item">
              <h3>Beyond the bill</h3>
              <p>Model breakdowns, session activity, MCP usage, code stats, and daemon-backed history. Where providers expose it.</p>
            </div></R>
            <R delay={0.30}><div className="feature-item">
              <h3>Open source, growing</h3>
              <p>A long list of supported providers, more every release. Yours missing? Open an issue or send a PR.</p>
            </div></R>
          </div>
        </div>
      </section>

      <section className="resources-section" id="resources">
        <div className="w">
          <R><h2 className="resources-title">More reading</h2></R>
          <div className="resources-grid">
            {resourceCards.map((card, i) => (
              <R key={card.id} delay={i * 0.06}>
                <a className="resource-card" href={card.href} onClick={() => trackCTA("resources", card.id)}>
                  <span className="resource-card__eyebrow">{card.eyebrow}</span>
                  <h3 className="resource-card__title">{card.title}</h3>
                  <p className="resource-card__desc">{card.description}</p>
                </a>
              </R>
            ))}
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
          <R><p className="demo__caption">Settings modal. Layout, graphs, thresholds, live preview.</p></R>
        </div>
      </section>

      {/* ── Install (left-heavy grid) ────────────────── */}
      <section className="install-section" id="install">
        <div className="w">
          <R>
            <div className="install-header">
              <h2 className="install-title">Get started</h2>
              <p className="install-desc">
                Install, run <code>openusage</code>, that's it. Auto-detection finds your installed tools and common API key env vars on its own.
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
            <p className="install-run">Then run <code>openusage</code></p>
          </R>
        </div>
      </section>

      </main>

      {/* ── Footer ───────────────────────────────────── */}
      <footer className="footer">
        <div className="w" style={{ display: "flex", justifyContent: "space-between", alignItems: "center", width: "100%" }}>
          <span>OpenUsage.sh · open source</span>
          <div className="footer__links">
            {analyticsAvailable ? (
              <button className="footer__link footer__button" type="button" onClick={openAnalyticsPreferences}>
                {analyticsPreferenceLabel()}
              </button>
            ) : null}
            <a className="footer__link" href="/docs/" onClick={() => trackCTA("footer", "docs")}>Docs</a>
            <a className="footer__link" href="https://github.com/janekbaraniewski/openusage" rel="noreferrer" target="_blank" onClick={() => trackOutbound("github", "footer")}>GitHub</a>
            <a className="footer__link" href="https://github.com/janekbaraniewski/openusage/releases" rel="noreferrer" target="_blank" onClick={() => trackOutbound("releases", "footer")}>Releases</a>
          </div>
        </div>
      </footer>
      {showConsentBanner ? (
        <div className="consent-banner" role="dialog" aria-live="polite" aria-label="Analytics preference">
          <p className="consent-banner__text">
            We'd like to count pageviews and clicks (no personal data, no extra cookies). Cool with you? You can flip this later from the footer.
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
