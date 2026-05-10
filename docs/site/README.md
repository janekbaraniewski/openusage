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

Every pull request that touches `docs/site/**` gets a unique preview URL via the
`docs-preview` GitHub Actions workflow, which deploys the built docs to
Cloudflare Pages and posts a sticky comment on the PR with the link.

### One-time setup

1. Create a Cloudflare Pages project:
   - Sign in to the [Cloudflare dashboard](https://dash.cloudflare.com)
   - **Workers & Pages → Create → Pages → Direct upload**
   - Project name: `openusage-docs`
   - Skip the initial upload step (the workflow will do it)

2. Generate an API token:
   - **My profile → API tokens → Create token**
   - Use the **Cloudflare Pages — Edit** template (or a custom token with `Account → Cloudflare Pages → Edit` permission)

3. Add two secrets to this GitHub repository (**Settings → Secrets and variables → Actions**):
   - `CLOUDFLARE_API_TOKEN` — the token from step 2
   - `CLOUDFLARE_ACC_ID` — visible in the Cloudflare dashboard sidebar

4. (Optional) Add a custom domain such as `docs-preview.openusage.sh` to the
   project so previews share a stable hostname pattern.

If the secrets are missing, the workflow still builds and uploads the static
artifact to the run page — it just skips the deploy + comment.

The `wrangler.toml` and `static/_headers` files in this directory document the
expected build output and HTTP headers. They're picked up by `wrangler pages
deploy build` whether the deploy runs from CI or from your laptop.

## Production deploy

The production site at [openusage.sh/docs](https://openusage.sh/docs/) is built by `.github/workflows/website.yaml` on every push to `main` that touches `docs/site/**` or `website/**`. The Docusaurus build is staged into `website/public/docs/` so the same GitHub-Pages deployment serves both the marketing site and the docs.

## License

MIT, same as OpenUsage.
