# Operations

## Startup

Use `run.sh` from the project root. The script handles the full boot sequence automatically.

```bash
./run.sh up
```

Boot sequence:

1. If the generated credential set is missing or inconsistent (`.env`, `deploy/auth.conf`, or required files under `deploy/keys/`): remove stale generated state, reset named Docker volumes, and regenerate credentials with `tools/bootstrap` (Go SDK only, no external tools)
2. Build and start the NATS container
3. Wait for NATS to be healthy (`/healthz`)
4. Build and start the `app` container (Nginx + API binary in one image)
5. The API ensures the account named by `JS_ACCOUNT_NAME` exists and connects its default JetStream session through that account

## Prerequisites

- Docker and Docker Compose
- Go (for first-run bootstrap only)

## NATS Operator Setup

NATS runs in Operator mode. On first run, `run.sh up` automatically generates all required credentials using `tools/bootstrap/main.go` (pure Go SDK — no `nsc` or any external NATS tool required).

### Generated files (gitignored)

| File        | Description                                                                                                        |
| ----------- | ------------------------------------------------------------------------------------------------------------------ |
| `auth.conf` | Operator JWT, system account public key, resolver config (`allow_delete: true` by default), and `resolver_preload` |
| `.env`      | API runtime settings plus `NATS_SYS_NKEY`, `JS_ACCOUNT_NAME`, and `OPERATOR_NKEY`                                  |
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

| Variable          | Default                  | Description                                                                                       |
| ----------------- | ------------------------ | ------------------------------------------------------------------------------------------------- |
| `API_PORT`        | `8080`                   | HTTP listen port                                                                                  |
| `JWT_SECRET`      | `change-me-local-secret` | Secret for signing console JWT tokens (expires in 12 hours)                                       |
| `NATS_URL`        | `nats://nats:4222`       | NATS server URL used by the API container                                                         |
| `NATS_SYS_NKEY`   | _(empty)_                | System account NKey seed used to mint the API's ephemeral NATS user at startup                    |
| `JS_ACCOUNT_NAME` | `CONSOLE_JS`             | Name of the default JetStream application account that the API looks up or creates on startup     |
| `OPERATOR_NKEY`   | _(empty)_                | Operator NKey seed for signing account JWTs pushed to the NATS full resolver                      |
| `ALLOWED_ORIGINS` | `http://localhost:5173`  | CORS allowed origin for the Nginx entrypoint                                                      |
| `DB_PATH`         | `/app/data/console.db`   | Path to SQLite database for persisting users and account signing seeds used for user creds export |

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
| `80`   | App entrypoint (Nginx: `<WEB_BASE_PATH>` web, `<WEB_BASE_PATH>/api` API) |

## Deployment Image (`deploy/Dockerfile`)

A single multi-stage image packages the full application:

1. **web-builder** (Node 22) — `npm run build` produces `/src/web/dist`
2. **api-builder** (Go 1.25) — compiles `nats-console-api` binary
3. **Final** (Nginx 1.27) — serves static files, proxies `<WEB_BASE_PATH>/api/` to `localhost:8080/api/`

`deploy/entrypoint.sh` starts the API process in the background and Nginx in the foreground. Nginx stdout/stderr is the container's main log stream (API errors go to stderr which Nginx captures).

### Environment variable loading

`deploy/docker-compose.yml` includes `env_file: ../.env` so the root `.env` file is loaded directly by Docker Compose. Variables declared in the `environment:` section override `.env` values, but only `API_PORT` and `DB_PATH` are set there; all others come from `.env`.

In remote deployments, provide the same variables via the platform's secret/env injection mechanism — no `.env` file required.

### NATS_URL for local vs remote

| Context        | Value                            |
| -------------- | -------------------------------- |
| Docker Compose | `nats://nats:4222` (service DNS) |
| Remote / bare  | `nats://<host>:4222`             |

`API_PORT` and `DB_PATH` are fixed inside the container and are not overridable via environment.

## Single Image Deployment (`deploy/dockerfile.console`)

For environments that inject runtime variables directly (for example Kubernetes), build and run one integrated image that contains web, API, and Nginx.

Build command:

```bash
docker build -f deploy/dockerfile.console -t nats-console:latest .
```

Runtime requirements:

- Inject API environment variables directly from the platform (`JWT_SECRET`, `NATS_URL`, `NATS_SYS_NKEY`, `OPERATOR_NKEY`, `JS_ACCOUNT_NAME`, `ALLOWED_ORIGINS`, `API_PORT`, `DB_PATH`)
- Mount persistent storage at `/app/data` when SQLite persistence is required

This image keeps the same runtime model as the default app image: API on `:8080` and Nginx serving web on `:80` with `/api` reverse proxy.

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
