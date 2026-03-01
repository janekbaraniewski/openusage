const repoUrl = "https://github.com/janekbaraniewski/openusage";
const licenseUrl = "https://github.com/janekbaraniewski/openusage/blob/main/LICENSE";

export default function Footer() {
  return (
    <footer className="footer">
      <div className="container footer-inner">
        <p>© {new Date().getFullYear()} OpenUsage</p>
        <div className="footer-links">
          <a href={repoUrl} target="_blank" rel="noopener noreferrer">
            GitHub
          </a>
          <a href={licenseUrl} target="_blank" rel="noopener noreferrer">
            MIT License
          </a>
        </div>
      </div>
    </footer>
  );
}
