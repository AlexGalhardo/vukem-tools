# Vukem Tools site

The public site of Vukem Tools: a landing page at `/` and the documentation under `/docs/`. It is static HTML built with Astro (`output: "static"`, directory `dist/`) and published to GitHub Pages by `.github/workflows/site.yml`. No server, no external script, font or analytics.

The shell is copied from the vukem documentation site (`vukem/site/`, the template of vukem.dev/docs) as SDD-010 section 1 describes. It was copied from vukem commit `c9a3fcca` (the last commit that touched `site/` in the vukem clone at `c4949ed0`, the head of its pull request 222).

## Run it

```sh
bun install
bun run dev        # http://localhost:4321/
bun run build      # static export to dist/
bun run preview
```

The site is served under the base path of its address. `SITE_URL` (default `https://tools.vukem.dev`, a custom domain on GitHub Pages, so the base is `/`) sets both the address and the base path; an address with a path (a project site) sets that path as the base. Every internal link goes through `withBase` in `lib/site.ts` or the `Link` component.

## Checks

| Command | What it checks |
| --- | --- |
| `npm run lint` | ESLint |
| `npm run check:mdx` | every page compiles, its components exist, its `/docs/` links resolve |
| `npm run build` then `npm run check:built` | only static files; no resource from another origin; every root-relative link carries the base path |
| `npm run test:scripts`, `npm run test:site` | the script tests |
| `npm run check:last-updated` | every docs page has a "Last updated" date at least as new as its last commit |

## Add a docs page

1. Create `content/docs/<dir>/<slug>.mdx` with `title` and `description` front matter.
2. List the slug in `content/navigation.ts`.
3. Run `npm run check:mdx`.
4. After the commit, run `npm run last-updated` (needs the full git history) and commit `content/last-updated.json`. Never edit it by hand.

## Status

The site is in English only: there is no language switcher and no translation check. The old `/pt-br/...` addresses redirect to the English pages (`redirects` in `astro.config.mjs`). Pages under `content/docs/en` are written by hand for this toolbox. `bun run reference` refuses to run: that generator belongs to the video-editor shell this site was copied from.
