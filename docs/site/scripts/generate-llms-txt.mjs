#!/usr/bin/env node
/**
 * Generate llms.txt and llms-full.txt from the docs/ tree.
 *
 * Output:
 *   static/llms.txt       — short index following https://llmstxt.org
 *   static/llms-full.txt  — every doc page concatenated, with frontmatter stripped
 *
 * Both files end up at /llms.txt and /llms-full.txt of whatever host serves the
 * build (i.e. openusage.sh/docs/llms.txt in production once mounted under /docs/).
 *
 * Run via: `node scripts/generate-llms-txt.mjs`
 * Hooked into `npm run build` via prebuild script.
 */

import {readdir, readFile, writeFile, mkdir} from 'node:fs/promises';
import {join, relative, dirname, sep} from 'node:path';
import {fileURLToPath} from 'node:url';

const __dirname = dirname(fileURLToPath(import.meta.url));
const ROOT = join(__dirname, '..');
const DOCS_DIR = join(ROOT, 'docs');
const STATIC_DIR = join(ROOT, 'static');

const SITE_URL = process.env.DOCS_PREVIEW === '1'
  ? '' // preview deploys: relative
  : 'https://openusage.sh/docs';

async function walk(dir) {
  const entries = await readdir(dir, {withFileTypes: true});
  const files = [];
  for (const e of entries) {
    const full = join(dir, e.name);
    if (e.isDirectory()) {
      files.push(...(await walk(full)));
    } else if (e.name.endsWith('.md') || e.name.endsWith('.mdx')) {
      files.push(full);
    }
  }
  return files;
}

function parseFrontmatter(content) {
  if (!content.startsWith('---')) return {data: {}, body: content};
  const end = content.indexOf('\n---', 3);
  if (end === -1) return {data: {}, body: content};
  const yaml = content.slice(3, end).trim();
  const body = content.slice(end + 4).replace(/^\n+/, '');
  const data = {};
  for (const line of yaml.split('\n')) {
    const m = line.match(/^(\w[\w-]*):\s*(.*)$/);
    if (m) {
      let val = m[2].trim();
      if (val.startsWith('"') && val.endsWith('"')) val = val.slice(1, -1);
      if (val.startsWith("'") && val.endsWith("'")) val = val.slice(1, -1);
      data[m[1]] = val;
    }
  }
  return {data, body};
}

function pathToUrl(absPath) {
  let rel = relative(DOCS_DIR, absPath).replaceAll(sep, '/');
  rel = rel.replace(/\.mdx?$/, '');
  if (rel === 'index') return SITE_URL + '/';
  if (rel.endsWith('/index')) rel = rel.slice(0, -6);
  return SITE_URL + '/' + rel + '/';
}

const SECTIONS = [
  {prefix: 'getting-started/', label: 'Getting Started'},
  {prefix: 'concepts/', label: 'Concepts'},
  {prefix: 'providers/', label: 'Providers'},
  {prefix: 'daemon/', label: 'Daemon & Telemetry'},
  {prefix: 'customization/', label: 'Customization'},
  {prefix: 'reference/', label: 'Reference'},
  {prefix: 'guides/', label: 'Guides'},
  {prefix: 'troubleshooting/', label: 'Troubleshooting'},
  {prefix: 'contributing/', label: 'Contributing'},
];

function bucketize(entries) {
  const buckets = new Map(SECTIONS.map(s => [s.prefix, []]));
  const root = [];
  const other = [];
  for (const entry of entries) {
    const rel = relative(DOCS_DIR, entry.absPath).replaceAll(sep, '/');
    if (rel === 'index.md' || rel === 'index.mdx') {
      root.push(entry);
      continue;
    }
    if (rel === 'faq.md' || rel === 'faq.mdx') {
      other.push(entry);
      continue;
    }
    let placed = false;
    for (const {prefix} of SECTIONS) {
      if (rel.startsWith(prefix)) {
        buckets.get(prefix).push(entry);
        placed = true;
        break;
      }
    }
    if (!placed) other.push(entry);
  }
  return {root, buckets, other};
}

const main = async () => {
  const files = (await walk(DOCS_DIR)).sort();
  const entries = [];
  for (const f of files) {
    const raw = await readFile(f, 'utf8');
    const {data, body} = parseFrontmatter(raw);
    entries.push({
      absPath: f,
      url: pathToUrl(f),
      title: data.title || '(untitled)',
      description: data.description || '',
      body: body.trim(),
    });
  }

  const {root, buckets, other} = bucketize(entries);

  // ── llms.txt: a friendly markdown index ─────────────────────────────────
  const lines = [
    '# OpenUsage',
    '',
    '> Local-first terminal dashboard for AI tool spend, quotas, and rate limits across 18 providers — Claude Code, Codex CLI, Cursor, Copilot, OpenRouter, OpenAI, Anthropic, and more.',
    '',
    'These docs cover installation, configuration, every provider integration, the optional background daemon, theming, the complete CLI and `settings.json` reference, troubleshooting, and contribution guidance.',
    '',
    'Project home: https://openusage.sh',
    'Source: https://github.com/janekbaraniewski/openusage',
    'Full docs (machine-readable): /llms-full.txt',
    '',
  ];

  for (const entry of root) {
    lines.push(`## ${entry.title}`, '', `- [${entry.title}](${entry.url}): ${entry.description}`, '');
  }

  for (const {prefix, label} of SECTIONS) {
    const items = buckets.get(prefix);
    if (!items.length) continue;
    lines.push(`## ${label}`, '');
    for (const entry of items) {
      lines.push(`- [${entry.title}](${entry.url}): ${entry.description}`);
    }
    lines.push('');
  }

  if (other.length) {
    lines.push('## More', '');
    for (const entry of other) {
      lines.push(`- [${entry.title}](${entry.url}): ${entry.description}`);
    }
    lines.push('');
  }

  await mkdir(STATIC_DIR, {recursive: true});
  await writeFile(join(STATIC_DIR, 'llms.txt'), lines.join('\n'));

  // ── llms-full.txt: full content, page by page ──────────────────────────
  const fullLines = [
    '# OpenUsage — full documentation',
    '',
    `Source: https://github.com/janekbaraniewski/openusage`,
    `Site: ${SITE_URL || 'https://openusage.sh/docs'}`,
    `Generated: ${new Date().toISOString()}`,
    '',
    '---',
    '',
  ];

  const allOrdered = [...root, ...SECTIONS.flatMap(s => buckets.get(s.prefix)), ...other];
  for (const entry of allOrdered) {
    fullLines.push(`# ${entry.title}`, '');
    fullLines.push(`URL: ${entry.url}`);
    if (entry.description) fullLines.push(`Description: ${entry.description}`);
    fullLines.push('', entry.body, '', '---', '');
  }

  await writeFile(join(STATIC_DIR, 'llms-full.txt'), fullLines.join('\n'));

  console.log(`[llms-txt] generated ${entries.length} entries → static/llms.txt + static/llms-full.txt`);
};

main().catch(err => {
  console.error('[llms-txt] failed:', err);
  process.exit(1);
});
