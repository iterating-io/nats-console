#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
COMPOSE_FILE="$SCRIPT_DIR/deploy/docker-compose.yml"
ROOT_ENV="$SCRIPT_DIR/.env"

# Load .env into the shell environment when present.
# docker compose reads env vars from the calling shell, so there is no
# env_file reference in docker-compose.yml. Remote deployments simply
# provide the same variables through the process environment.
load_env() {
    if [[ -f "$ROOT_ENV" ]]; then
        set -a
        # shellcheck source=/dev/null
        source "$ROOT_ENV"
        set +a
    fi
}

usage() {
    cat <<EOF
Usage: $(basename "$0") <command>

Commands:
  up        Build images and start all services in the background
  down      Stop and remove containers
  restart   Restart all services
  logs      Follow logs of all services (Ctrl+C to stop)
  build     Rebuild all images without starting
  ps        Show running containers
  clean     Stop containers, remove images and volumes (destructive)
  help      Show this help message
EOF
}

case "${1:-help}" in
    up)
        echo ">> Starting nats-console..."

        AUTH_CONF="$SCRIPT_DIR/deploy/auth.conf"
        KEYS_DIR="$SCRIPT_DIR/deploy/keys"

        # Step 1: bootstrap if any required file is missing or auth.conf is outdated.
        # Partial state (e.g. .env present but keys/ missing) is also treated as
        # a broken setup — remove everything and regenerate to keep files in sync.
        needs_bootstrap=false
        [[ ! -f "$ROOT_ENV" ]] && needs_bootstrap=true
        [[ ! -f "$AUTH_CONF" ]] && needs_bootstrap=true
        [[ ! -f "$KEYS_DIR/operator.nk" ]] && needs_bootstrap=true
        [[ ! -f "$KEYS_DIR/sys-account.nk" ]] && needs_bootstrap=true
        [[ ! -f "$KEYS_DIR/sys-user.nk" ]] && needs_bootstrap=true
        [[ ! -f "$KEYS_DIR/sys-account.jwt" ]] && needs_bootstrap=true
        [[ -f "$KEYS_DIR/js-account.nk" ]] && needs_bootstrap=true
        [[ -f "$KEYS_DIR/js-account.jwt" ]] && needs_bootstrap=true

        # Identity consistency check: sys-account.nk public key must match
        # system_account in auth.conf. If they differ, the config has drifted
        # (e.g. auth.conf was restored from backup while keys/ was not).
        if [[ "$needs_bootstrap" == "false" ]]; then
            SYS_ACCOUNT_IN_CONF="$(grep 'system_account:' "$AUTH_CONF" | sed 's/.*"\(.*\)".*/\1/')"
            SYS_ACCOUNT_FROM_KEY="$(cd "$SCRIPT_DIR/api" && go run ../tools/bootstrap/main.go --print-pubkey "$KEYS_DIR/sys-account.nk" 2>/dev/null || true)"
            if [[ -n "$SYS_ACCOUNT_FROM_KEY" && "$SYS_ACCOUNT_IN_CONF" != "$SYS_ACCOUNT_FROM_KEY" ]]; then
                echo ">> Auth identity mismatch detected (auth.conf <-> keys/). Forcing re-bootstrap..."
                needs_bootstrap=true
            fi
        fi

        if [[ "$needs_bootstrap" == "true" ]]; then
            echo ">> Bootstrap required. Removing stale credentials and regenerating..."

            # Bring down containers first so volumes are not in use.
            docker compose -f "$COMPOSE_FILE" down 2>/dev/null || true

            rm -f "$ROOT_ENV" "$AUTH_CONF" \
                  "$SCRIPT_DIR/deploy/.env.nats" "$SCRIPT_DIR/deploy/.env.operator"
            rm -rf "$KEYS_DIR"
            # Clear persisted user data too. Re-bootstrapping creates a new
            # operator/system-account identity, so old SQLite user records should
            # not be carried into the new environment.
            docker volume rm deploy_console-data 2>/dev/null || true
            docker volume rm deploy_nats-resolver 2>/dev/null || true

            (cd "$SCRIPT_DIR/api" && go run ../tools/bootstrap/main.go --out-dir ../deploy)

            echo ">> Operator bootstrapped."
        fi

        load_env

        # Step 2: start NATS only (build image)
        docker compose -f "$COMPOSE_FILE" up --build -d nats

        # Step 3: wait for NATS to be ready
        echo ">> Waiting for NATS..."
        # First, wait for port to be accessible (quick check)
        for i in $(seq 1 30); do
            if (echo > /dev/tcp/localhost/4222) 2>/dev/null; then
                break
            fi
            sleep 0.5
        done
        # Then, wait for healthz endpoint (with more time for JWT resolution)
        for i in $(seq 1 60); do
            if curl -sf http://localhost:8222/healthz > /dev/null 2>&1; then
                break
            fi
            sleep 1
            if [[ $i -eq 60 ]]; then
                echo "ERROR: NATS did not become ready in time."
                exit 1
            fi
        done

        # Step 3: start remaining services
        docker compose -f "$COMPOSE_FILE" up --build -d

        echo ""
        echo ">> Services:"
        echo "   App : http://localhost"
        echo "   API : http://localhost/api/health"
        echo "   NATS: nats://localhost:4222  monitoring: http://localhost:8222"
        ;;
    down)
        echo ">> Stopping nats-console..."
        docker compose -f "$COMPOSE_FILE" down
        ;;
    restart)
        echo ">> Restarting nats-console..."
        docker compose -f "$COMPOSE_FILE" down
        "$0" up
        ;;
    logs)
        docker compose -f "$COMPOSE_FILE" logs -f
        ;;
    build)
        echo ">> Building images..."
        docker compose -f "$COMPOSE_FILE" build
        ;;
    ps)
        docker compose -f "$COMPOSE_FILE" ps
        ;;
    clean)
        echo ">> WARNING: This will remove all containers, images, volumes, and generated credentials."
        read -r -p "Continue? [y/N] " confirm
        if [[ "$confirm" =~ ^[Yy]$ ]]; then
            docker compose -f "$COMPOSE_FILE" down --rmi local --volumes --remove-orphans
            # Force-remove named volumes in case compose left them behind
            docker volume rm deploy_nats-resolver deploy_console-data 2>/dev/null || true
            # Remove generated credential files so next `up` re-bootstraps
            rm -f "$SCRIPT_DIR/.env" \
                  "$SCRIPT_DIR/deploy/.env.nats" \
                  "$SCRIPT_DIR/deploy/.env.operator" \
                  "$SCRIPT_DIR/deploy/auth.conf"
            rm -rf "$SCRIPT_DIR/deploy/keys"
            echo ">> Cleaned."
        else
            echo ">> Aborted."
        fi
        ;;
    help|--help|-h)
        usage
        ;;
    *)
        echo "Unknown command: $1"
        usage
        exit 1
        ;;
esac
