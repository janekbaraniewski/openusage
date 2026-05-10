import type {ReactElement} from 'react';
import Layout from '@theme/Layout';
import Link from '@docusaurus/Link';

export default function NotFound(): ReactElement {
  return (
    <Layout title="Page not found" description="That page doesn't exist in OpenUsage docs.">
      <main className="container margin-vert--xl">
        <div className="row">
          <div className="col col--6 col--offset-3">
            <h1 style={{fontFamily: 'JetBrains Mono, monospace', letterSpacing: '-0.02em'}}>
              404 — page not found
            </h1>
            <p style={{color: 'var(--ifm-color-content-secondary)'}}>
              The page you're looking for might have moved, been renamed, or never
              existed. Try one of these jumping-off points:
            </p>
            <ul style={{listStyle: 'none', padding: 0, lineHeight: '2.2'}}>
              <li>
                <Link to="/">Docs home</Link>
              </li>
              <li>
                <Link to="/getting-started/install/">Install</Link>
              </li>
              <li>
                <Link to="/getting-started/quickstart/">Quickstart</Link>
              </li>
              <li>
                <Link to="/providers/">Provider catalog</Link>
              </li>
              <li>
                <Link to="/reference/cli/">CLI reference</Link>
              </li>
              <li>
                <Link to="/faq/">FAQ</Link>
              </li>
              <li>
                <a href="https://github.com/janekbaraniewski/openusage/issues">
                  Report a broken link →
                </a>
              </li>
            </ul>
          </div>
        </div>
      </main>
    </Layout>
  );
}
