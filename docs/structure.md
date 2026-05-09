# Project Structure

This document describes the repository layout and explains the purpose of each directory and key file.

## Overview

NATS Console is a monorepo containing three main parts:

| Directory          | Stack                     | Role                                              |
| ------------------ | ------------------------- | ------------------------------------------------- |
| `api/`             | Go                        | REST API server and WebSocket endpoint            |
| `web/`             | React + TypeScript + Vite | Browser-based management UI                       |
| `tools/bootstrap/` | Go                        | One-time credential generator for first-run setup |
| `deploy/`          | Docker Compose            | Local development and production deployment       |
| `docs/`            | Markdown                  | Project documentation                             |

---

## Directory Tree

```
nats-console/
├── run.sh                    # Entry point: bootstrap + start all services
│
├── api/                      # Go API server
│   ├── go.mod                # Go module definition
│   ├── cmd/server/main.go    # Entrypoint: loads config, starts HTTP server
│   └── internal/
│       ├── auth/jwt.go       # Console JWT issuance and validation
│       ├── config/config.go  # Environment variable loading
│       ├── httpapi/server.go # Route definitions, handlers, WebSocket
│       └── store/store.go    # In-memory + SQLite data layer
│
├── web/                      # React UI
│   ├── src/
│   │   ├── main.tsx          # React entry point
│   │   ├── App.tsx           # Route definitions
│   │   ├── context/
│   │   │   └── AuthContext.tsx   # JWT token + role state (sessionStorage)
│   │   ├── hooks/
│   │   │   └── useApiBase.ts     # Derives API prefix from router base path
│   │   ├── pages/            # Route-level page components
│   │   │   ├── DashboardLayout.tsx  # Outer layout with sidebar
│   │   │   ├── LoginPage.tsx
│   │   │   ├── AccountsPage.tsx     # Operator + Account + User management
│   │   │   └── StreamsPage.tsx      # JetStream streams and consumers
│   │   ├── types/
│   │   │   └── index.ts             # Shared TypeScript types (Operator, Account, User)
│   │   └── components/       # Feature-scoped UI components
│   │       ├── Auth/
│   │       ├── Account/
│   │       ├── Operator/
│   │       ├── Streams/
│   │       ├── Consumers/
│   │       ├── Dashboard/
│   │       └── Sidebar/
│   ├── index.html
│   ├── vite.config.ts
│   └── package.json
│
├── tools/
│   └── bootstrap/main.go     # Generates NATS operator, account, and user keys/JWTs
│
├── deploy/
│   ├── docker-compose.yml    # Services: NATS and app (Nginx + API binary)
│   ├── nats.conf             # Static NATS configuration template
│   ├── auth.conf             # Generated on first run (gitignored)
│   └── keys/                 # Generated NKey seed files (gitignored)
│
└── docs/
    ├── architecture.md       # System design, data models, API contracts
    ├── operations.md         # Deployment, environment variables, startup procedure
    └── structure.md          # This file — directory and file layout
```

---

## Key Concepts

### `api/internal/store`

The store layer manages two types of data differently:

- **Operators and Accounts** — loaded from the NATS resolver at startup and held in memory. They reflect NATS server state and are not written by the API.
- **Users** — created and deleted through the API. Stored in a SQLite database (`console.db`) so they survive server restarts.

### `api/internal/httpapi`

All HTTP routes and handlers live here, including:

- REST endpoints for operators, accounts, users, streams, and consumers
- WebSocket handler at `/api/ws` for live server events

### `web/src/pages`

Pages are route-level components. Each page imports and composes one or more components but does not contain inline UI logic of its own.

### `web/src/components`

Components are organized by NATS entity:

| Directory    | Responsibility                        |
| ------------ | ------------------------------------- |
| `Auth/`      | Login form                            |
| `Account/`   | Account list, user list, account form |
| `Operator/`  | Operator list                         |
| `Streams/`   | Stream list, stream creation form     |
| `Consumers/` | Consumer list, consumer creation form |
| `Dashboard/` | Live event feed, publish form         |
| `Sidebar/`   | Navigation sidebar                    |

### `web/src/context/AuthContext`

Stores the console JWT token and the user's role (`admin`, `operator`, `viewer`) in `sessionStorage`. Components read this context to enforce role-based UI restrictions.

### `web/src/hooks/useApiBase`

Returns the API prefix derived from the app base path. All API calls in components use this hook so API routes follow `<WEB_BASE_PATH>/api` automatically.

---

## Contributing

1. Read [docs/architecture.md](architecture.md) to understand the system design and data model.
2. Read [docs/operations.md](operations.md) for local setup and environment variables.
3. Check `web/README.md` for web-specific development instructions.
4. Follow the component organization rules: one directory per NATS entity, pages import components and do not contain inline logic.
