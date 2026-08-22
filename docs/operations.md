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

## Account Provisioning Notes

- Creating an account via `POST /api/v1/accounts` also creates a default per-account user named `stream-reader`.
- This user is stored in SQLite (`console.db`) with fixed permissions for stream info/message-read access:
    - publish allow: `$JS.API.STREAM.INFO.*`, `$JS.API.STREAM.MSG.GET.*`
    - publish deny: `$JS.API.CONSUMER.>`, `$JS.API.STREAM.CREATE.>`, `$JS.API.STREAM.UPDATE.>`, `$JS.API.STREAM.DELETE.>`, `$JS.API.STREAM.PURGE.>`, `$JS.API.STREAM.MSG.DELETE.>`
    - subscribe allow: `_INBOX.>`

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

### GitHub Actions image publishing

`.github/workflows/build-ghcr.yml` builds the console image for `linux/arm64`
when manually dispatched from `main`. It pushes the image to:

```text
ghcr.io/iterating-io/nats-console/console:<short-sha>
```

The workflow uses `WEB_BASE_PATH=/console`, which matches the production
ingress at `https://nats.iterating.io/console`. It does not access runtime
secrets or the private Kubernetes registry. The infrastructure repository pulls
the resulting ARM64 image and copies it to the private registry before Helm
deployment.

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

### Message API: `context deadline exceeded` (2026-06-10)

A transient `context deadline exceeded` error observed when calling the
`GET /api/v1/streams/{stream}/messages/last` endpoint was traced to NATS
JetStream permission rejections. Root cause: the API previously cached a
single `*nats.Conn` per account without separating the authentication identity
(ephemeral account-signed user vs stored per-account users such as
`stream-reader`). This led to JetStream API requests being issued with an
identity that lacked the necessary publish permission to `$JS.API.STREAM.MSG.GET.*`,
producing permission violations and timeouts.

Fix (2026-06-10): the connection cache was split per account into an `ephemeral`
connection and a `users` map so that requests requiring a stored user's
permissions authenticate with that user's identity. Key files changed:
`api/internal/jetstream/handler.go`, `api/internal/jetstream/handler_internal.go`.
Transient session notes were recorded at `.github/sessions/session-20260610-103500.md`
and have been removed after consolidation into this document.

This recreates only the NATS container and clears the container-local JetStream store used by this setup, while keeping generated auth files and named volumes intact.

## Session: session-20260610-153000 (finalized 2026-06-10)

Summary:

- Problem: On the Messages page, switching accounts could leave the UI polling a stream that no longer existed, producing repeated "stream not found" errors and continuing background polling.

Actions and commits (chronological):

- `1421754` — fix(messages): stop polling when stream missing; avoid repeated 'stream not found' errors
- `bc83d84` — fix(messages): require explicit stream selection; clear stream on account change
- `37fa354` — fix(messages): clear message/seq/error when switching streams
- `8a5cb02` — chore(ui): label account and streams in MessagesPage for clarity and accessibility
- `459e373` — chore(ui): make list-name-btn fill row so list items are easier to click
- `86f9dfb` — feat(ui): make selected stream visually prominent; add 'Selected' badge and show active stream in message panel
- `7d2fc94` — fix(ui): require confirmation before delete actions to avoid accidental removals
- `400acda` — chore(sessions): update session file with performed actions and commit history

Files changed (summary):

- `web/src/pages/MessagesPage.tsx`
- `web/src/App.css`
- `web/src/components/Streams/StreamList.tsx`
- `web/src/components/Streams/StreamDetail.tsx`
- `web/src/components/Consumers/ConsumerList.tsx`

Status:

- Session work consolidated into this documentation entry and the repository commits listed above.
- Temporary session file `.github/sessions/session-20260610-153000.md` has been removed as part of session close.

Notes:

- If any accidental deletions happened during testing, provide details (resource type, identifier, approximate time) and I will investigate recovery options.
