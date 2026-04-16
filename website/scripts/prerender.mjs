/**
 * Post-build prerender: renders the SPA to static HTML for instant LCP.
 */

import { launch } from "puppeteer";
import { readFileSync, writeFileSync, existsSync, statSync, readdirSync } from "fs";
import { join, dirname, extname } from "path";
import { fileURLToPath } from "url";
import { createServer } from "http";

const __dirname = dirname(fileURLToPath(import.meta.url));
const distDir = join(__dirname, "..", "dist");
const indexPath = join(distDir, "index.html");

const mime = { ".html":"text/html",".js":"text/javascript",".css":"text/css",".json":"application/json",".svg":"image/svg+xml",".gif":"image/gif",".png":"image/png",".webm":"video/webm",".mp4":"video/mp4",".txt":"text/plain",".xml":"application/xml",".webp":"image/webp" };

const basePath = "/";

function serve(dir) {
  return createServer((req, res) => {
    let pathname = decodeURIComponent(new URL(req.url, "http://x").pathname);
    // Strip base path prefix
    if (pathname.startsWith(basePath)) pathname = "/" + pathname.slice(basePath.length);
    let p = join(dir, pathname);
    if (existsSync(p) && statSync(p).isDirectory()) p = join(p, "index.html");
    if (!existsSync(p)) { res.writeHead(404); return res.end(); }
    const ct = mime[extname(p)] || "application/octet-stream";
    res.writeHead(200, { "Content-Type": ct, "Cache-Control": "no-cache" });
    res.end(readFileSync(p));
  });
}

const server = serve(distDir);
await new Promise(r => server.listen(0, r));
const port = server.address().port;
console.log(`[prerender] dist/ on :${port}`);

const browser = await launch({ headless: true, args: ["--no-sandbox", "--disable-gpu"] });
const page = await browser.newPage();
await page.goto(`http://localhost:${port}/`, { waitUntil: "networkidle0", timeout: 30000 });
await new Promise(r => setTimeout(r, 1000));

const count = await page.evaluate(() => document.querySelector("#root")?.childNodes.length ?? 0);
if (count === 0) {
  console.error("[prerender] ERROR: React did not render. Skipping prerender.");
  await browser.close();
  server.close();
  process.exit(0); // don't fail build
}

const rootHTML = await page.$eval("#root", el => el.innerHTML);
let html = readFileSync(indexPath, "utf8");
html = html.replace('<div id="root"></div>', `<div id="root">${rootHTML}</div>`);
writeFileSync(indexPath, html, "utf8");
console.log(`[prerender] injected ${rootHTML.length} chars`);

await browser.close();
server.close();
