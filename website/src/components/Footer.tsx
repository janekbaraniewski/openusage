export default function Footer() {
  return (
    <footer className="footer">
      <div className="container footer-inner">
        <p>© {new Date().getFullYear()} OpenUsage</p>
        <div className="footer-links">
          <a
            href="https://github.com/janekbaraniewski/openusage"
            target="_blank"
            rel="noopener noreferrer"
          >
            GitHub
          </a>
          <a
            href="https://github.com/janekbaraniewski/openusage/blob/main/LICENSE"
            target="_blank"
            rel="noopener noreferrer"
          >
            MIT
          </a>
        </div>
      </div>
    </footer>
  );
}
