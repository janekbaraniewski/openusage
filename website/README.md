# Website (GitHub Pages)

This folder contains the OpenUsage marketing site.

## Branch flow

- Source branch: `website-main`
- Pages branch: `gh-pages`
- Site root for publishing: `website/`

## Local preview

From repo root:

```bash
cd website
python3 -m http.server 4173
```

Open `http://localhost:4173`.

## Publish `website/` to `gh-pages`

From the `website-main` branch:

```bash
git subtree split --prefix website -b gh-pages-tmp
git push -f origin gh-pages-tmp:gh-pages
git branch -D gh-pages-tmp
```

Then in GitHub repo settings:

- Pages source: `Deploy from a branch`
- Branch: `gh-pages`
- Folder: `/ (root)`

## Notes

- `.nojekyll` is included to avoid Jekyll processing.
- Assets required by the site are copied into `website/assets/`.
