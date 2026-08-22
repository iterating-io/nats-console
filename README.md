# NATS Console

A web-based management console for connecting to an existing NATS Operator setup.

Console connects to a running NATS server and provides UI for:

- Account and user management
- JetStream stream and consumer management
- User credentials export (Copy or Download)
- On-demand publish testing

## Prerequisites

NATS server must be configured with:

- **Operator mode** enabled (operator JWT and system account configured)
- **Full resolver** (`type: full` in resolver config) for account JWT persistence
- `allow_delete: true` in resolver config (for UI account deletion support)
- Pre-provisioned system account NKey seed (`NATS_SYS_NKEY`)
- Pre-provisioned operator NKey seed (`OPERATOR_NKEY`)

## Features

- Account and user lifecycle management with pub/sub and publish allow rules
- Account creation auto-provisions a `stream-reader` user with stream-info/message-read only permissions
- User credentials export and creds file download
- JetStream stream and consumer CRUD
- Dashboard publish test and live heartbeat
- Role-based access: admin, operator, viewer

## Deployment

The `Build and publish ARM64 console image` GitHub Actions workflow builds the
ARM64 production image from `main` when run manually. It publishes the image to
GHCR using the short commit SHA as its tag:

```text
ghcr.io/iterating-io/nats-console/console:<short-sha>
```

The image is built with `WEB_BASE_PATH=/console` for the
`https://nats.iterating.io/console` ingress path. The infrastructure repository
copies that image to the internal Kubernetes registry before deployment.

For a local production-image build:

```bash
docker build \
  --build-arg WEB_BASE_PATH=/console \
  -f deploy/dockerfile.console \
  -t nats-console:latest .
```

Run with pre-provisioned NATS credentials:

```bash
docker run -d \
  -e NATS_URL=nats://nats.example.com:4222 \
  -e NATS_SYS_NKEY=Sxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx \
  -e OPERATOR_NKEY=SOxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx \
  -e ADMIN_ID=admin \
  -e ADMIN_PASSWORD=securepassword \
  -e ALLOWED_ORIGINS=https://console.example.com \
  -p 9222:9222 \
  -v console-data:/app/data \
  nats-console:latest
```

Environment variables must be provided from your existing NATS operator/account setup:

- `NATS_URL`: NATS server connection URL
- `NATS_SYS_NKEY`: System account NKey seed (pre-provisioned in NATS)
- `OPERATOR_NKEY`: Operator NKey seed (pre-provisioned in NATS)
- `ADMIN_ID`: Console admin username (e.g., `admin`)
- `ADMIN_PASSWORD`: Console admin password
- `ALLOWED_ORIGINS`: CORS allowed origins (e.g., your domain)
- `WEB_BASE_PATH`: URL base path (default: `/`, optional)

Access the console at `https://console.example.com` or `http://localhost:9222`.

| Port | Purpose                       |
| ---- | ----------------------------- |
| 9222 | Console web interface (Nginx) |

## Usage

1. Access console at configured URL (e.g., http://localhost:9222)
2. Login with admin account
3. Manage accounts and users under your operator
4. Create JetStream streams and consumers
5. Export user credentials from UI (Copy or Download)
6. Test publish operations in Dashboard

## Account Deletion

Account delete is controlled by NATS resolver config (`allow_delete` in `deploy/auth.conf`):

- If `allow_delete: true`: accounts can be deleted via UI
- If `allow_delete: false`: delete action is disabled and API rejects delete requests
- System account is always protected

## Docker Image

`deploy/dockerfile.console` builds a single image containing:

- React web frontend
- Go API server
- Nginx reverse proxy

See Deployment section for usage.

## Environment Variables

Configuration through environment variables:

| Variable        | Default    | Purpose                             |
| --------------- | ---------- | ----------------------------------- |
| NATS_URL        | (required) | NATS server connection URL          |
| NATS_SYS_NKEY   | (required) | System account NKey seed            |
| OPERATOR_NKEY   | (required) | Operator NKey seed                  |
| ADMIN_ID        | (required) | Admin console username              |
| ADMIN_PASSWORD  | (required) | Admin console password              |
| ALLOWED_ORIGINS | `*`        | CORS allowed origins                |
| WEB_BASE_PATH   | `/`        | URL base path (e.g., /nats-console) |

## Account Management

- Manage accounts and users under existing operators
- Export credentials via UI (Copy or Download)
- Grant JetStream capability to accounts

## Docker Compose Example

For quick testing with an external NATS:

```yaml
services:
    console:
        image: nats-console:latest
        environment:
            NATS_URL: nats://nats.example.com:4222
            NATS_SYS_NKEY: ${NATS_SYS_NKEY}
            OPERATOR_NKEY: ${OPERATOR_NKEY}
            ADMIN_ID: admin
            ADMIN_PASSWORD: ${ADMIN_PASSWORD}
            ALLOWED_ORIGINS: https://console.example.com
        ports:
            - "9222:9222"
        volumes:
            - console-data:/app/data

volumes:
    console-data:
```

Run with:

```bash
export NATS_SYS_NKEY="Sxxxxxxxxxx..."
export OPERATOR_NKEY="SOxxxxxxxxxx..."
export ADMIN_PASSWORD="securepassword"
docker-compose up
```

## Notes

- Requires NATS Operator mode with FULL resolver and pre-provisioned `NATS_SYS_NKEY` and `OPERATOR_NKEY`
- Database (`/app/data/console.db` inside container) persists user records; mount volume to preserve across restarts
- Port 9222 is the only exposed port; API (9322) runs internally behind Nginx

## Documentation

- [docs/operations.md](docs/operations.md) — Deployment and configuration details
- [docs/architecture.md](docs/architecture.md) — System design and API reference

## Development

For developers:

- [web/README.md](web/README.md) — Web UI development setup
- Read [docs/architecture.md](docs/architecture.md) for design and API contracts
- `api` is a Go module; `web` is a Vite + React + TypeScript app
