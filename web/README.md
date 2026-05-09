# NATS Console — Web UI

React + TypeScript + Vite frontend for NATS Console.

## Prerequisites

- Node.js 20+
- The API server running at `http://localhost:8080` (see root `README.md` for setup)

## Development Setup

```bash
cd web
npm install
npm run dev
```

The dev server starts at `http://localhost:5173`.

`WEB_BASE_PATH` controls both web mount path and API prefix:

- `/` -> web on `/`, API on `/api`
- `/console` -> web on `/console`, API on `/console/api`
- `/test/` -> web on `/test`, API on `/test/api`

The Vite dev server proxies `<WEB_BASE_PATH>/api` to `http://localhost:8080`, so no separate API base variable is needed.

```bash
WEB_BASE_PATH=/console npm run dev
```

## Build

```bash
npm run build   # outputs to dist/
npm run preview # preview the production build locally
```

## Project Layout

```
src/
├── main.tsx              # React entry point
├── App.tsx               # Route definitions (react-router-dom)
├── context/
│   └── AuthContext.tsx   # JWT token + role state
├── hooks/
│   └── useApiBase.ts     # Derives API prefix from base path
├── pages/                # One file per route
│   ├── DashboardLayout.tsx
│   ├── LoginPage.tsx
│   ├── AccountsPage.tsx
│   ├── OperatorsPage.tsx
│   ├── StreamsPage.tsx
│   ├── ConsumersPage.tsx
│   ├── AuthenticationPage.tsx
│   └── DashboardPage.tsx
└── components/           # Feature components, one directory per NATS entity
    ├── Auth/
    ├── Account/
    ├── Operator/
    ├── Streams/
    ├── Consumers/
    ├── Dashboard/
    └── Sidebar/
```

## Architecture Notes

### Routing

Routes are defined once in `App.tsx`. Each route maps to exactly one page component under `src/pages/`.

### Pages vs Components

- **Pages** (`src/pages/`) are route-level components. They import and compose feature components but contain no inline UI logic themselves.
- **Components** (`src/components/`) are organized by NATS entity (Account, Streams, etc.) and hold the actual UI and data-fetching logic.

### Authentication

`AuthContext` stores the console JWT token and the user's role (`admin`, `operator`, `viewer`) in `sessionStorage`. Any component that needs to make authenticated requests or enforce role-based rendering calls `useAuth()`.

```ts
const { token, role } = useAuth();
```

### API Calls

All components that call the API use the `useApiBase()` hook to get the base URL:

```ts
const base = useApiBase();
const res = await fetch(`${base}/api/v1/accounts`, {
    headers: { Authorization: `Bearer ${token}` },
});
```

This ensures API calls always follow `<WEB_BASE_PATH>/api` and are never hardcoded in components.

## Adding a New Feature

1. Create a directory under `src/components/<EntityName>/`.
2. Put list, form, and detail components inside it.
3. Create or update the corresponding page in `src/pages/`.
4. Add a route in `App.tsx` if needed.
5. Add a sidebar entry in `src/components/Sidebar/Sidebar.tsx`.

```js
// eslint.config.js
import reactX from "eslint-plugin-react-x";
import reactDom from "eslint-plugin-react-dom";

export default defineConfig([
    globalIgnores(["dist"]),
    {
        files: ["**/*.{ts,tsx}"],
        extends: [
            // Other configs...
            // Enable lint rules for React
            reactX.configs["recommended-typescript"],
            // Enable lint rules for React DOM
            reactDom.configs.recommended,
        ],
        languageOptions: {
            parserOptions: {
                project: ["./tsconfig.node.json", "./tsconfig.app.json"],
                tsconfigRootDir: import.meta.dirname,
            },
            // other options...
        },
    },
]);
```
