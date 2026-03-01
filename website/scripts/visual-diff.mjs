import { chromium } from "playwright";
import pixelmatch from "pixelmatch";
import { PNG } from "pngjs";
import fs from "node:fs/promises";
import path from "node:path";

const ROOT = path.resolve(path.dirname(new URL(import.meta.url).pathname), "..");
const OUT_DIR = path.join(ROOT, ".playwright-mcp", "diff-run");
const BASE_URL = process.env.BASE_URL ?? "http://127.0.0.1:4173";

const modules = [
  { id: "claude", file: "claudecode.png" },
  { id: "cursor", file: "cursor.png" },
  { id: "openrouter", file: "openrouter.png" },
  { id: "copilot", file: "copilot.png" },
  { id: "gemini", file: "gemini.png" },
  { id: "codex", file: "codex.png" },
];

function loadPng(buffer) {
  return PNG.sync.read(buffer);
}

function cropPng(source, width, height) {
  const out = new PNG({ width, height });
  PNG.bitblt(source, out, 0, 0, width, height, 0, 0);
  return out;
}

async function ensureDir(dir) {
  await fs.mkdir(dir, { recursive: true });
}

async function run() {
  await ensureDir(OUT_DIR);

  const browser = await chromium.launch({ headless: true });
  const page = await browser.newPage({ viewport: { width: 2200, height: 1600 } });

  const results = [];

  for (const module of modules) {
    await page.goto(`${BASE_URL}/#modules`, { waitUntil: "networkidle" });
    await page.addStyleTag({
      content: `
        .topbar { display: none !important; }
        * { animation: none !important; transition: none !important; }
      `,
    });
    await page.locator(`[data-module-id="${module.id}"]`).click();
    await page.waitForTimeout(180);

    const locator = page.locator('[data-testid="native-module-shell"]');
    const shotPath = path.join(OUT_DIR, `${module.id}.png`);
    await locator.screenshot({ path: shotPath });

    const sourcePath = path.join(ROOT, "public", "assets", module.file);

    const [shotBuf, srcBuf] = await Promise.all([
      fs.readFile(shotPath),
      fs.readFile(sourcePath),
    ]);

    const shot = loadPng(shotBuf);
    const src = loadPng(srcBuf);

    const width = Math.min(shot.width, src.width);
    const height = Math.min(shot.height, src.height);

    const shotCrop = cropPng(shot, width, height);
    const srcCrop = cropPng(src, width, height);
    const diff = new PNG({ width, height });

    const mismatch = pixelmatch(
      shotCrop.data,
      srcCrop.data,
      diff.data,
      width,
      height,
      {
        threshold: 0.12,
        alpha: 0.6,
        aaColor: [0, 255, 255],
        diffColor: [255, 0, 120],
        includeAA: true,
      },
    );

    const total = width * height;
    const ratio = (mismatch / total) * 100;

    const diffPath = path.join(OUT_DIR, `${module.id}.diff.png`);
    await fs.writeFile(diffPath, PNG.sync.write(diff));

    results.push({
      module: module.id,
      source: module.file,
      shot: `${shot.width}x${shot.height}`,
      sourceDim: `${src.width}x${src.height}`,
      compared: `${width}x${height}`,
      mismatch,
      ratio,
      diffPath,
    });
  }

  await browser.close();

  results.sort((a, b) => a.ratio - b.ratio);

  console.log("module\tmismatch%\tshot\tsource\tcompared");
  for (const row of results) {
    console.log(
      `${row.module}\t${row.ratio.toFixed(2)}\t${row.shot}\t${row.sourceDim}\t${row.compared}`,
    );
  }

  const reportPath = path.join(OUT_DIR, "report.json");
  await fs.writeFile(reportPath, JSON.stringify(results, null, 2));
  console.log(`\nSaved report: ${reportPath}`);
}

run().catch((err) => {
  console.error(err);
  process.exit(1);
});
