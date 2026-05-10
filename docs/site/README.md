# OpenUsage docs site

User-facing documentation for OpenUsage, built with [Docusaurus 3](https://docusaurus.io/). Hosted at [openusage.sh/docs](https://openusage.sh/docs/).

## Layout

- `docs/` — markdown source for every page
- `src/css/custom.css` — OpenUsage brand theme
- `static/img/` — favicon, logo, screenshots
- `docusaurus.config.ts` — site config (baseUrl, navbar, footer, OG metadata)
- `sidebars.ts` — sidebar structure

## Develop

```bash
npm install
npm run start
```

The dev server opens at [localhost:3000](http://localhost:3000) on the `/docs/` base. Hot reload is on.

## Build

```bash
npm run build
```

Output goes to `build/`. The directory is self-contained and can be served from any static host. The whole tree assumes it's mounted at `/docs/` — the `baseUrl` is set in `docusaurus.config.ts`.

## Deploy to openusage.sh

The marketing site at [openusage.sh](https://openusage.sh) lives in `../../website/` (the `website/` directory at the repo root). Drop the built docs in its `public/docs/` directory:

```bash
npm run build
rm -rf ../../website/public/docs
cp -r build ../../website/public/docs
```

Then build and deploy the marketing site as usual.

## Type-check

```bash
npm run typecheck
```

## PR previews via Cloudflare Pages

Cloudflare Pages is free for OSS, auto-deploys every PR with a unique preview URL, and posts a comment on the PR with the link. To wire it up:

1. Sign in to the [Cloudflare dashboard](https://dash.cloudflare.com) and pick **Workers & Pages → Create → Pages**
2. Connect this GitHub repo
3. Configure the build:
   - **Framework preset:** Docusaurus
   - **Root directory:** `docs/site`
   - **Build command:** `npm run build`
   - **Build output:** `build`
   - **Node version:** 22 (set under env vars or pulled from `wrangler.toml`)
4. Add a custom domain (`docs.openusage.sh` is the suggested one) — or use the default `pages.dev` URL until ready

The `wrangler.toml` and `static/_headers` files in this directory document the expected build output and HTTP headers. They're also picked up if you deploy via `wrangler pages deploy build`.

## Production deploy

The production site at [openusage.sh/docs](https://openusage.sh/docs/) is built by `.github/workflows/website.yaml` on every push to `main` that touches `docs/site/**` or `website/**`. The Docusaurus build is staged into `website/public/docs/` so the same GitHub-Pages deployment serves both the marketing site and the docs.

## License

MIT, same as OpenUsage.
