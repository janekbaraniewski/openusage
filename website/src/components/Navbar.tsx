export default function Navbar() {
  return (
    <header className="topbar">
      <div className="container topbar-inner">
        <a href="/" className="brand">
          <span className="brand-dot" aria-hidden="true" />
          openusage
        </a>

        <nav className="topnav" aria-label="Main navigation">
          <a href="#install">Install</a>
          <a
            href="https://github.com/janekbaraniewski/openusage#readme"
            target="_blank"
            rel="noopener noreferrer"
          >
            Docs
          </a>
          <a
            href="https://github.com/janekbaraniewski/openusage"
            target="_blank"
            rel="noopener noreferrer"
            className="button button-solid topnav-cta"
          >
            GitHub
          </a>
        </nav>
      </div>
    </header>
  );
}
