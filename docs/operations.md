# Operations

## Startup

Use `run.sh` from the project root. The script handles the full boot sequence automatically.

```bash
./run.sh up
```

Boot sequence:

1. If the generated credential set is missing or inconsistent (`.env`, `deploy/auth.conf`, or required files under `deploy/keys/`): remove stale generated state, reset named Docker volumes, and regenerate credentials with `tools/bootstrap` (Go SDK only, no external tools, local development only)
2. Build and start the NATS container
3. Wait for NATS to be healthy (`/healthz`)
4. Build and start the `app` container (Nginx + API binary in one image)
5. The API ensures the account named `CONSOLE_JS` exists and connects its default JetStream session through that account

## Prerequisites

- Docker and Docker Compose
- Go (for first-run bootstrap only)

## NATS Operator Setup

NATS runs in Operator mode. On first run, `run.sh up` automatically generates all required credentials using `tools/bootstrap/main.go` (pure Go SDK — no `nsc` or any external NATS tool required).

This bootstrap flow is intended for local development only.
For production, use your existing NATS deployment where Operator and required accounts are already provisioned, and inject `NATS_SYS_NKEY` and `OPERATOR_NKEY` from that environment.

### Generated files (gitignored)

| File        | Description                                                                                                        |
| ----------- | ------------------------------------------------------------------------------------------------------------------ |
| `auth.conf` | Operator JWT, system account public key, resolver config (`allow_delete: true` by default), and `resolver_preload` |
| `.env`      | API runtime settings plus `NATS_SYS_NKEY` and `OPERATOR_NKEY`                                                      |
| `keys/`     | NKey seed files for operator, system account, and system user (backup/recovery only)                               |

To regenerate credentials manually, delete `.env`, `deploy/auth.conf`, and `deploy/keys/`, then run `./run.sh up` again.

When bootstrap regenerates credentials, `run.sh up` also removes the `deploy_nats-resolver` and `deploy_console-data` Docker volumes so stale resolver JWTs and user rows cannot leak into the new identity set.

If legacy `deploy/keys/js-account.nk` or `deploy/keys/js-account.jwt` files are found, `run.sh up` treats them as stale bootstrap artifacts, clears the generated state, and reboots into the current model where the API creates the JetStream application account on demand.

`run.sh` also performs an identity consistency check before startup:

- Reads `system_account` from `deploy/auth.conf`
- Derives the public key from `deploy/keys/sys-account.nk`
- If they do not match, forces full bootstrap and clears volumes

## Environment Variables

### API (`api/`)

| Variable          | Default    | Description                                                                   |
| ----------------- | ---------- | ----------------------------------------------------------------------------- |
| `NATS_URL`        | (required) | NATS server URL                                                               |
| `NATS_SYS_NKEY`   | (required) | System account NKey seed used to mint the API's ephemeral NATS user           |
| `OPERATOR_NKEY`   | (required) | Operator NKey seed for signing account JWTs and console authentication tokens |
| `ADMIN_ID`        | (required) | Admin console login username                                                  |
| `ADMIN_PASSWORD`  | (required) | Admin console login password                                                  |
| `ALLOWED_ORIGINS` | `*`        | CORS allowed origins for web frontend                                         |

Database path is fixed at `/app/data/console.db` inside container.

### Web (`web/`)

| Variable        | Default | Description                                                                 |
| --------------- | ------- | --------------------------------------------------------------------------- |
| `WEB_BASE_PATH` | `/`     | App mount path. API path is derived automatically as `<WEB_BASE_PATH>/api`. |

## Volumes

| Volume          | Mount in container    | Purpose                                                                |
| --------------- | --------------------- | ---------------------------------------------------------------------- |
| `nats-resolver` | `/data/nats/resolver` | Persists Account JWTs across NATS restarts                             |
| `console-data`  | `/app/data`           | Persists SQLite database (`console.db`) with users across API restarts |

## Ports

| Port   | Service                                                                  |
| ------ | ------------------------------------------------------------------------ |
| `4222` | NATS client                                                              |
| `8222` | NATS monitoring                                                          |
| `9222` | App entrypoint (Nginx: `<WEB_BASE_PATH>` web, `<WEB_BASE_PATH>/api` API) |
| `9322` | API server (internal, proxied by Nginx)                                  |

## Deployment Image (`deploy/Dockerfile`)

A single multi-stage image packages the full application:

1. **web-builder** (Node 22) — `npm run build` produces `/src/web/dist`
2. **api-builder** (Go 1.25) — compiles `nats-console-api` binary
3. **Final** (Nginx 1.27) — serves static files, proxies `<WEB_BASE_PATH>/api/` to `localhost:9322/api/`

`deploy/entrypoint.sh` starts the API process in the background and Nginx in the foreground. Nginx stdout/stderr is the container's main log stream (API errors go to stderr which Nginx captures).

### Environment variable loading

`deploy/docker-compose.yml` includes `env_file: ../.env` so the root `.env` file is loaded directly by Docker Compose. All environment variables come from `.env`.

In remote deployments, provide the same variables via the platform's secret/env injection mechanism — no `.env` file required.

### NATS_URL for local vs remote

| Context        | Value                            |
| -------------- | -------------------------------- |
| Docker Compose | `nats://nats:4222` (service DNS) |
| Remote / bare  | `nats://<host>:4222`             |

`API_PORT` (9322) and `DB_PATH` (`/app/data/console.db`) are fixed constants inside the container.

## Single Image Deployment (`deploy/dockerfile.console`)

For environments that inject runtime variables directly (for example Kubernetes), build and run one integrated image that contains web, API, and Nginx.

Build command:

```bash
docker build -f deploy/dockerfile.console -t nats-console:latest .
```

Runtime requirements:

- Inject API environment variables directly from the platform (`NATS_URL`, `NATS_SYS_NKEY`, `OPERATOR_NKEY`, `ADMIN_ID`, `ADMIN_PASSWORD`, `ALLOWED_ORIGINS`)
- Mount persistent storage at `/app/data` for SQLite database (`/app/data/console.db`)

This image keeps the same runtime model: API on `:9322` and Nginx serving web on `:9222` with `/api` reverse proxy.

## Troubleshooting

### `./run.sh up` stops at `Waiting for NATS...`

Symptom:

- NATS container is running and port `4222` is open
- `http://localhost:8222/healthz` returns `503`
- NATS logs repeatedly show a JetStream recovery warning like `could not be recovered`

Cause:

- NATS process is up, but JetStream cannot recover one or more stored stream artifacts, so `/healthz` stays unavailable.

Recovery:

```bash
docker compose -f deploy/docker-compose.yml stop nats
docker compose -f deploy/docker-compose.yml rm -f nats
docker compose -f deploy/docker-compose.yml up -d nats
./run.sh up
```

This recreates only the NATS container and clears the container-local JetStream store used by this setup, while keeping generated auth files and named volumes intact.
