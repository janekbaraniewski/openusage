import { useEffect, useRef, useState } from "react";

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

  useEffect(() => {
    const onScroll = () => setScrolled(window.scrollY > 100);
    onScroll();
    window.addEventListener("scroll", onScroll, { passive: true });
    return () => window.removeEventListener("scroll", onScroll);
  }, []);

  useEffect(() => {
    if (!copied) return;
    const t = setTimeout(() => setCopied(""), 1500);
    return () => clearTimeout(t);
  }, [copied]);

  async function copy(cmd) {
    try { await navigator.clipboard.writeText(cmd); setCopied(cmd); }
    catch { setCopied(""); }
  }

  return (
    <>
      {/* ── Nav ──────────────────────────────────────── */}
      <nav className={`nav${scrolled ? " nav--visible" : ""}`}>
        <NavLogo />
        <div className="nav__right">
          <a className="nav__link" href="#providers">Providers</a>
          <a className="nav__link" href="https://github.com/janekbaraniewski/openusage" rel="noreferrer" target="_blank">GitHub</a>
          <a className="nav__cta" href="#install">Install</a>
        </div>
      </nav>

      {/* ── Hero (left-aligned) ──────────────────────── */}
      <main>
      <section className="hero">
        <div className="w" style={{ textAlign: 'left' }}>
          <R><Banner className="banner" /></R>
          <R delay={0.15}>
            <h1 className="hero__title">
              Know what your AI tools cost.
            </h1>
          </R>
          <R delay={0.25}>
            <p className="hero__sub">
              The coding agent usage dashboard you've been looking for.
              Auto-detects 17 providers. Zero config. Your terminal.
            </p>
          </R>
          <R delay={0.35}>
            <div className="hero__actions">
              <a className="btn btn--fill" href="#install">Get started</a>
              <a className="btn btn--ghost" href="https://github.com/janekbaraniewski/openusage" rel="noreferrer" target="_blank">GitHub →</a>
            </div>
          </R>
        </div>
      </section>

      {/* ── Pitch (alternating alignment) ────────────── */}
      <section className="pitch">
        <div className="w">
          <R as="p" className="pitch__line">
            <em>Auto-detects</em> your AI coding tools and API keys.
          </R>
          <R className="pitch__line" delay={0.12}>
            <p className="pitch__line" style={{margin:0}}>
              Shows <em>live spend, quotas,</em> and <em>per-model usage</em> at a glance.
            </p>
          </R>
          <R className="pitch__line" delay={0.24}>
            <p className="pitch__line" style={{margin:0}}>
              Zero config required — just run <code>openusage</code>.
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
          <R><p className="demo__caption">dashboard · detail · compare · analytics</p></R>
        </div>
      </section>

      {/* ── Side-by-side video ────────────────────────────── */}
      <section className="demo">
        <div className="w">
          <R>
            <p className="demo__caption" style={{ textAlign: 'left', marginBottom: 16, fontSize: '1rem', color: 'var(--text-2)' }}>
              Run it side-by-side with your coding agent
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

      {/* ── Settings video ───────────────────────────────── */}
      <section className="demo">
        <div className="w">
          <R>
            <p className="demo__caption" style={{ textAlign: 'left', marginBottom: 16, fontSize: '1rem', color: 'var(--text-2)' }}>
              Configurable tile sections. Customizable detail graphs. Customizable dashboards. Time windows. Thresholds. 17 built-in themes.
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
          <R><p className="demo__caption">Settings modal — tile sections, detail graphs, themes, and live preview</p></R>
        </div>
      </section>

      {/* ── Providers (asymmetric: title left, grid below) ── */}
      <section className="prov-section" id="providers">
        <div className="w">
          <R>
            <div className="prov-header">
              <h2 className="prov-header__title">17 providers</h2>
              <p className="prov-header__sub">
                Coding agents, API platforms, and local tools.<br />One surface.
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

      {/* ── Install (left-heavy grid) ────────────────── */}
      <section className="install-section" id="install">
        <div className="w">
          <R>
            <div className="install-header">
              <h2 className="install-title">Get started</h2>
              <p className="install-desc">
                One command. Auto-detects every provider and API key on first run.
                No config file needed.
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
            <a className="footer__link" href="https://github.com/janekbaraniewski/openusage" rel="noreferrer" target="_blank">GitHub</a>
            <a className="footer__link" href="https://github.com/janekbaraniewski/openusage/releases" rel="noreferrer" target="_blank">Releases</a>
          </div>
        </div>
      </footer>
    </>
  );
}
