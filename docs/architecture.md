# Architecture

## Monorepo Layout

- `api/`: Go API server
- `web/`: React + Vite console UI
- `deploy/`: local Docker Compose
- `docs/`: project documentation

## NATS Connection

The API connects to NATS using a dynamically generated user credential:

- `NATS_SYS_NKEY` — sys account seed (`SA...`) stored in `.env`
- At startup, the server generates a fresh user keypair via `nkeys.CreateUser()`, signs a user JWT with the sys account seed, and uses that ephemeral credential for the NATS connection
- The base bootstrap only provisions operator and system identities; the JetStream application account is managed later by the API at runtime

## Runtime Model

- UI uses REST for request/response operations.
- UI uses WebSocket for near-real-time server heartbeat/events.
- API connects to NATS and JetStream.

## API Scope (V1)

- `POST /api/auth/login`: local JWT issuance
- `GET /api/health`: API and NATS connectivity status
- `GET /api/ws?token=...`: heartbeat/event channel

### JetStream

| Method | Path                                          | Description                   |
| ------ | --------------------------------------------- | ----------------------------- |
| GET    | `/api/v1/streams`                             | List stream names             |
| POST   | `/api/v1/streams`                             | Create a new stream           |
| DELETE | `/api/v1/streams/{name}`                      | Delete a stream               |
| GET    | `/api/v1/streams/{name}/consumers`            | List consumers for a stream   |
| POST   | `/api/v1/streams/{name}/consumers`            | Create a consumer on a stream |
| DELETE | `/api/v1/streams/{name}/consumers/{consumer}` | Delete a consumer             |

All JetStream endpoints accept an optional `?accountPublicKey=` query parameter. When provided, the server routes operations through the matching account's NATS connection instead of the default `JS_ACCOUNT_NAME` managed account.

#### Default JetStream account lifecycle

- On startup, `EnsureJetStreamAccountAndConnect()` looks for an account whose name matches `JS_ACCOUNT_NAME` under the current operator.
- If that account does not exist, the API creates it, signs its JWT with the operator NKey, pushes it to the NATS full resolver, and opens the default JetStream connection.
- The bootstrap step does not pre-create this account or store `js-account.*` seed/JWT artifacts anymore.

#### Account-scoped JetStream connections

- The `Server` struct holds an `accountConns map[string]*nats.Conn` connection pool keyed by account public key.
- `getOrCreateAccountConn(publicKey, signingKeySeed)` reuses an existing live connection or creates a new one (5 s timeout), then stores it in the pool.
- `jetStreamForRequest(w, r)` reads `accountPublicKey` from the query string: if empty, falls back to `s.jsConn` (the account named by `JS_ACCOUNT_NAME`); otherwise looks up the account in memory, validates `JSEnabled`, retrieves the signing key, and delegates to `getOrCreateAccountConn`.
- Per-request NATS connection creation (and the associated deadline-exceeded errors) is eliminated by connection reuse.

### Publish

- `POST /api/v1/publish`: publish a test message (role-based, admin/operator only)
- `POST /api/v1/publish/as-user`: publish as a specific user with subject permission check

## Operator / Account / User Management

### Data Model

- **Operator** — top-level namespace (`name`); loaded from NATS resolver at startup and kept in-memory; immutable (read-only in API)
- **Account** — belongs to an operator; kept in-memory, populated from NATS; has `publishAllow []string` and `subscribeAllow []string` subject rules; immutable (read-only in API)
- **User** — belongs to an account identified by its NATS account public key; persisted to SQLite (`console.db`); NATS keypair auto-generated on creation (`publicKey` stored); has `publishAllow []string` subject rules

### Storage

- **Operators**: In-memory at startup, derived from accounts loaded from NATS resolver via `LoadFromNATS()`. Read-only.
- **Accounts**: Not stored in the database. On creation or permission update, the API signs a new account JWT with the operator NKey and immediately pushes it to the NATS full resolver via `$SYS.REQ.CLAIMS.UPDATE`. On startup, `LoadFromNATS()` restores accounts from the resolver into memory. Resolver persistence is backed by the Docker named volume `nats-resolver`.
- **Users**: Persisted to SQLite database (`console.db`) and keyed by `(operator, account_public_key, user name)`. Survives across server restarts and is backed by Docker volume `console-data`.

### REST Endpoints

| Method      | Path                                                                        | Description                                                                                                       |
| ----------- | --------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------- | --- | ---- | ---------------------------------------------------------- | ---------------------------------------------------------------------- | --- | --- | --------------- | ------------------------------------------- |
| GET         | `/api/v1/operators`                                                         | List operators (in-memory from NATS)                                                                              |
| GET         | `/api/v1/accounts`                                                          | List accounts (in-memory from NATS)                                                                               |
| POST        | `/api/v1/accounts`                                                          | Create account: generates NKey, signs JWT with operator key, pushes to NATS resolver via `$SYS.REQ.CLAIMS.UPDATE` |
| DELETE      | `/api/v1/accounts/{operator}/{accountPublicKey}`                            | Delete account from NATS resolver via `$SYS.REQ.CLAIMS.DELETE`; then cleanup local users/signing keys             |
| POST/DELETE | `/api/v1/accounts/{operator}/{name}/publish-allow`                          | Add / remove account publish subjects; re-signs and pushes JWT to resolver                                        |
| POST/DELETE | `/api/v1/accounts/{operator}/{name}/subscribe-allow`                        | Add / remove account subscribe subjects; re-signs and pushes JWT to resolver                                      |
| GET/POST    | `/api/v1/accounts/{operator}/{accountPublicKey}/users`                      | List / create users for the account identified by public key (SQLite persisted)                                   |
| DELETE      | `/api/v1/accounts/{operator}/{accountPublicKey}/users/{user}`               | Delete user (SQLite persisted)                                                                                    |
| GET         | `/api/v1/accounts/{operator}/{accountPublicKey}/users/{user}/creds`         | Export decorated NATS `.creds` payload for a persisted user                                                       |
| POST/DELETE | `/api/v1/accounts/{operator}/{accountPublicKey}/users/{user}/publish-allow` | Add / remove user publish subjects (SQLite persisted)                                                             |     | POST | `/api/v1/accounts/{operator}/{accountPublicKey}/jetstream` | Enable or disable JetStream for an account (`{"enabled": true/false}`) |     | GET | `/api/v1/users` | List all users across all accounts (SQLite) |
| POST        | `/api/v1/publish/as-user`                                                   | Publish as a specific user with subject permission check                                                          |

### Account Delete Capability Guard

- API probes resolver delete support using `$SYS.REQ.CLAIMS.DELETE` and returns `capabilities.accountDelete` in `GET /api/v1/accounts`.
- Frontend uses that capability to disable account delete actions when resolver delete is not enabled.
- API still enforces server-side protection and returns `412` when delete is disabled.
- System account is always protected from delete and reported with `isSystem: true`.

### Publish Permission Logic (`/api/v1/publish/as-user`)

- Checks both account-level and user-level `publishAllow` lists.
- Subject matching supports `>` (multi-token wildcard) and `*` (single-token wildcard).
- If `publishAllow` is empty for a record, all subjects are permitted.
- Returns `403` if subject is denied by either account or user rule.

### User Keypair

- On user creation, server auto-generates a NATS keypair via `nkeys.CreateUser()`.
- The API persists user seeds and account signing seeds in SQLite for later `.creds` export.
- User creds can be exported on demand through the creds endpoint; legacy users created before seed persistence return an explicit error and must be recreated.

## Frontend Structure

- `pages/AccountsPage.tsx` — unified Operator + Account + User management screen; handles 401 redirect on token expiration
- `pages/StreamsPage.tsx` — JetStream stream list, create, and delete; includes account selector to scope operations per account
- `pages/ConsumersPage.tsx` — consumer list, create, and delete scoped to a selected stream
- `components/Operator/OperatorList` — read-only clickable rows, highlights selected operator
- `components/Account/AccountForm` — creates accounts (in-memory only); uses currently selected operator
- `components/Account/AccountList` — filtered by selected operator; shows publish/subscribe subjects, user list, and JetStream enable/disable toggle
- Grant JS API subjects are intentionally hidden from the account allow lists in the UI to prevent accidental per-subject deletion; users manage those subjects only through the Grant/Revoke JS API action.
- `components/Account/UserList` — per-account user management; shows public key and publish subjects
- `components/Streams/StreamList` — lists streams with delete action
- `components/Streams/StreamForm` — creates a new stream; includes account selector to create stream under a specific account
- `components/Streams/StreamDetail` — stream detail with consumers, JS API access grant section, and `onNotFound` callback for stale stream deselection; publish/subscribe grants and account-level JetStream toggle stay on the Accounts screen
- `components/Consumers/ConsumerList` — lists consumers for a selected stream with delete action
- `components/Consumers/ConsumerForm` — creates a consumer with name and filter subject
- Accounts responses include `publicKey`, and all user CRUD requests are scoped by that public key rather than by account name.
- `components/Dashboard/PublishForm` — user-select dropdown; sends via `/api/v1/publish/as-user`
- **Token expiration handling**: All pages detect 401 responses and redirect to login (`/`)
- Sidebar groups: Authentication (`Operators / Accounts` → `/dashboard/accounts`), JetStream (`Streams / Consumers` → `/dashboard/streams`)
- `types/index.ts` — shared TypeScript types (`Operator`, `Account`, `User`) imported by all pages and components; eliminates duplicate local type definitions

## Security Notes

- Current auth is local static users for bootstrap only. JWT tokens expire in 12 hours.
- Production should replace local login with OIDC/SSO and use longer token TTL or refresh tokens.
- JWT secret must be rotated and managed via secret manager.
- Expired tokens trigger automatic redirect to login page.

## Next Expansion

- Stream/consumer CRUD endpoints
- Audit/event history persistence
- Fine-grained RBAC by resource/action
