# OMM Progressive Web App

The OMM frontend is a Vue 3 + TypeScript single-page app, built with Vite and
served by the `meshd` daemon. It runs as an installable Progressive Web App
(offline-capable, mobile-friendly) and is the same codebase intended to be
embedded in LuCI.

## Stack

- **Vue 3** (Composition API, `<script setup>`)
- **Vue Router** (hash history — see below)
- **Vite** + **vite-plugin-pwa** (manifest + service worker)
- **Vitest** + **@vue/test-utils** for tests
- **ESLint** + **Prettier** for linting/formatting

## How it is served

`meshd` embeds the built `dist/` directory via `//go:embed` (see
[`../web/embed.go`](embed.go)) and serves it as a catch-all route, so the whole
product ships as a single Go binary. The REST API is mounted at the same origin
on specific paths (`/status`, `/homes`, `/nodes`, …), which always take
precedence over the SPA catch-all.

The router uses **hash history** (`/#/homes`), so the backend only ever serves
`index.html` and static assets — there are no server-side route collisions with
the API and no server rewrite rules are required.

## Development

```bash
cd web
npm install
npm run dev        # Vite dev server on :5173, proxies API calls to meshd
```

By default the dev server proxies API requests to `http://localhost:8080`.
Point it elsewhere with `MESHD_API_TARGET`:

```bash
MESHD_API_TARGET=http://192.168.1.1:8080 npm run dev
```

## Scripts

| Command          | Description                                  |
| ---------------- | -------------------------------------------- |
| `npm run dev`    | Start the Vite dev server with API proxy     |
| `npm run build`  | Type-check (`vue-tsc`) and build into `dist` |
| `npm run test`   | Run the Vitest unit tests once               |
| `npm run lint`   | Lint with ESLint (zero warnings allowed)     |
| `npm run format` | Format `src` with Prettier                   |

## Production build

The repo build script ([`../scripts/build.sh`](../scripts/build.sh)) builds the
frontend before compiling `meshd`, so the latest assets are embedded:

```bash
./scripts/build.sh
```

## Layout

```
web/
├── index.html
├── public/icons/        # PWA icons
├── src/
│   ├── api/             # REST client (ApiClient, injectable fetch)
│   ├── components/      # Reusable presentational components
│   ├── composables/     # useAsync loading/error/data helper
│   ├── utils/           # Formatting helpers
│   ├── views/           # Dashboard, Homes, Nodes, Topology
│   ├── App.vue
│   ├── main.ts
│   └── router.ts
├── embed.go             # Go embed of dist/ (part of the meshd module)
└── handler.go           # SPA HTTP handler (DI'd fs.FS, SPA fallback)
```

## Status

Implemented: app shell + navigation, API client, Dashboard (daemon status +
home/node counts), Homes (list, create, set-active), Home detail with profile
editor, Enrollment (pending-adoption approve/reject queue + join-another-home),
Nodes list and node detail, PWA manifest + service worker.

Pending: the Topology view is a placeholder until `meshd` exposes a
`/topology` endpoint; it will then render the mesh graph with Cytoscape.js as
described in the spec. A first-boot onboarding wizard and a settings screen are
also future work.
