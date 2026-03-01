const docsUrl = "https://github.com/janekbaraniewski/openusage#readme";
const repoUrl = "https://github.com/janekbaraniewski/openusage";

export default function Navbar() {
  return (
    <header className="topbar">
      <div className="container topbar-inner">
        <a href="#top" className="brand" aria-label="OpenUsage home">
          <span className="brand-orb" aria-hidden="true" />
          <span>openusage</span>
        </a>

        <nav className="topnav" aria-label="Main navigation">
          <a href="#modules">Modules</a>
          <a href="#side-by-side">Side by Side</a>
          <a href="#install">Install</a>
          <a href={docsUrl} target="_blank" rel="noopener noreferrer">
            Docs
          </a>
          <a
            href={repoUrl}
            target="_blank"
            rel="noopener noreferrer"
            className="button button-solid nav-cta"
          >
            GitHub
          </a>
        </nav>
      </div>
    </header>
  );
}
