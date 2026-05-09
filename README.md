# NATS Console

A web-based management console for NATS Operator mode.

It helps you run and operate a local NATS environment from a browser, including:

- Operator, account, and user lifecycle management
- JetStream stream and consumer management
- User-scoped publish testing
- On-demand NATS user creds export

## Features

- Operator, account, and user management with account pub/sub and user publish allow rules
- Account delete with resolver capability guard and system-account protection
- User creds export with Copy and Download actions
- JetStream stream and consumer CRUD
- Dashboard publish test and live server heartbeat
- Role-based access: admin, operator, viewer

## Quick Start

Prerequisites:

- Docker and Docker Compose
- Go (for bootstrap generation)

```bash
./run.sh up
```

First run behavior:

1. Generates operator/system credentials and config in `deploy/` and root `.env`
2. Starts NATS first and waits for health
3. Starts API and the Nginx entrypoint

No external nsc tooling is required.

| Service      | URL                         |
| ------------ | --------------------------- |
| App          | http://localhost            |
| API health   | http://localhost/api/health |
| NATS monitor | http://localhost:8222       |

## Default Accounts

Use these local development accounts to sign in:

| Username | Password | Role     | Permissions                    |
| -------- | -------- | -------- | ------------------------------ |
| admin    | admin    | admin    | Full access including publish  |
| operator | operator | operator | Manage accounts/users, publish |
| viewer   | viewer   | viewer   | Read-only                      |

## Common Workflow

1. Start stack: ./run.sh up
2. Login as admin
3. Create account under selected operator
4. Set account publish/subscribe subjects and create users with publish subjects
5. Export creds from user row when needed
6. Test publish as user in Dashboard

## Account Delete Behavior

Account delete is controlled by NATS resolver config.

- If allow_delete is true in deploy/auth.conf, delete is enabled
- If allow_delete is false, the UI disables delete action and API rejects delete requests
- System account is always protected and cannot be deleted

## Generated Artifacts

The startup/bootstrap flow manages these files:

- deploy/auth.conf
- .env
- deploy/keys/operator.nk
- deploy/keys/sys-account.nk
- deploy/keys/sys-user.nk
- deploy/keys/sys-account.jwt

The startup script also checks auth identity consistency between auth.conf and keys.
If mismatch is detected, it re-bootstraps and clears stale volumes.

## Commands

```bash
./run.sh down      # Stop all services
./run.sh restart   # Rebuild and restart
./run.sh logs      # Follow logs
./run.sh clean     # Remove containers, images, and volumes
```

## Troubleshooting

Operators or accounts are empty after restart:

- Cause: auth artifacts drift (auth.conf, .env, keys)
- Action: run ./run.sh up again (bootstrap consistency check will repair)

Deleted account reappears after restart:

- Cause: resolver delete disabled
- Action: set allow_delete: true in deploy/auth.conf and restart

Too many old SYS accounts appear:

- Cause: stale resolver volume from previous identity sets
- Action: run ./run.sh clean, then ./run.sh up

## Documentation

| Document                                     | Description                                             |
| -------------------------------------------- | ------------------------------------------------------- |
| [docs/structure.md](docs/structure.md)       | Directory layout and how each part fits together        |
| [docs/architecture.md](docs/architecture.md) | System design, data models, and API reference           |
| [docs/operations.md](docs/operations.md)     | Environment variables, deployment, and credential setup |
| [web/README.md](web/README.md)               | Web UI development setup and component guide            |

## Notes

- This project is for local/development operations.
- Generated keys and the root `.env` contain sensitive seeds. Do not commit them.

## Contributing

1. Read [docs/structure.md](docs/structure.md) for a tour of the codebase.
2. Read [docs/architecture.md](docs/architecture.md) for design decisions and API contracts.
3. api is a Go module and web is a Vite + React + TypeScript app.
4. PRs and issues are welcome.
