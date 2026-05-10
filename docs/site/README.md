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

## License

MIT, same as OpenUsage.
