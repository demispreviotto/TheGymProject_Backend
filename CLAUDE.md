# CLAUDE.md: Backend Blueprint & System Architecture

Single source of truth for the **TheGymProject Backend** (Go). All generated code must follow the patterns, constraints and architecture below. The companion frontend (Angular + Supabase) lives in the sibling folder `../TheGymProject`; its `CLAUDE.md` owns the product domain and database schema.

---

## Current Development State

**Active phase:** Milestone 0 — scaffold. The API builds and exposes `/healthz`, `/readyz`, `/metrics` and `GET /v1/me`.

| Milestone | Status | Summary |
|-----------|--------|---------|
| 0 — Skeleton | ✅ Done | Layered layout, Supabase JWT auth, Prometheus metrics, JSON logs, Docker + compose (api, prometheus, grafana) |
| 1 — Cache | ⏳ Next | Redis cache-aside for `loadPlan`; invalidate on plan edit; hit-rate dashboard |
| 2 — Load balancer | Planned | N API replicas behind Nginx/Traefik, health checks, failover drill |
| 3 — Queue + workers | Planned | Outbox table → queue → worker for push/email/1RM stats; retries, DLQ, idempotency |
| 4 — Storage + CDN | Planned | Logos/media to S3-compatible storage (MinIO) with signed URLs |
| 5 — Read replica | Planned | Streaming replication; route reads to replica; observe lag |
| 6 — Sharding | Planned | Shard `workout_logs` by `tenant_id` (learning exercise, do last) |
| 7 — Observability | Continuous | Metrics → traces (OpenTelemetry) → log aggregation |

**Branches:** `main` is production (protected, PR-only). `dev` is the integration branch; work lands there first.

**Start dev:** start Supabase from the frontend repo (`pnpm exec supabase start`), then here: `cp .env.example .env` → `make run`.

**Ports:** API `8080` · Prometheus `9090` · Grafana `3000` · Supabase Postgres `54322` (from frontend repo).

---

## Quick Reference

| Action | Command |
|--------|---------|
| Run API | `make run` (or `go run ./cmd/api`) |
| Test | `make test` (`go test ./... -race -cover`) |
| Vet | `make lint` (`go vet ./...`) |
| Format | `gofmt -w .` |
| Tidy deps | `make tidy` |
| Full local stack | `make up` / `make down` |

| Need to change... | Edit... |
|-------------------|---------|
| A new env var | `internal/config/config.go` + `.env.example` |
| New business rule | `internal/service/` |
| New SQL / table access | `internal/repository/` |
| New endpoint / middleware | `internal/transport/http/` |
| New entity / sentinel error | `internal/domain/` |
| Wiring / new dependency | `cmd/api/main.go` |
| Database schema | **Frontend repo** `supabase/migrations/` — never here |

---

## 0. Critical Execution Constraints

### Production Safety — HARD STOPS
Never run these without explicit user confirmation in the same conversation turn. State _"This will affect production. Confirm?"_ and wait.

| Action | Risk |
|--------|------|
| `docker push`, `fly deploy`, `railway up`, or any deploy command | Replaces the live API |
| Any SQL write against the production database | Direct production mutation |
| Changing production secrets/env (`SUPABASE_JWT_SECRET`, DB URL) | Locks out all users or leaks access |
| Force-push / rewrite history on `main` | Destroys shared history |

Deployment state is **not recorded in this file**; check the source of truth (`git log origin/main`, the hosting provider) before assuming anything.

### Schema Ownership
- The database schema, RLS policies and migrations are owned by the **frontend repo**. This service **never** creates, alters or drops schema objects.
- A new column/table needed here is requested as a migration in the frontend repo first (Expand/Contract), then consumed here.
- The API connects with a DB role that **bypasses RLS**, so RLS is *not* protecting API reads. Every query must scope by the authenticated user / tenant explicitly in the service layer (see Security).

### Structural Rigidity
- No rogue refactoring of `go.mod`, `Dockerfile`, `docker-compose.yml` or CI unless asked. Adding a dependency is a deliberate decision: say why, prefer the standard library first.
- Provide complete modified functions/files; never use `// ... rest of code` placeholders.
- Folder convention (below) is strict.

---

## 1. Technical Stack

- **Language:** Go (module `github.com/demispreviotto/gymproject-backend`)
- **HTTP:** `net/http` + `chi` router
- **DB:** PostgreSQL (Supabase) via `pgx/v5` + `pgxpool`; plain SQL, no ORM
- **Auth:** Supabase-issued JWT (HS256 now; JWKS when moving to asymmetric keys)
- **Observability:** `log/slog` JSON logs, Prometheus metrics, Grafana (traces via OpenTelemetry later)
- **Packaging:** multi-stage Dockerfile → distroless static image
- **Frontend contract:** the Angular app calls this API with the user's Supabase access token as `Authorization: Bearer`.

---

## 2. Architecture: Layered, Hexagonal-Ready

```
cmd/api                  composition root — wiring only, no logic
internal/domain          entities, value types, sentinel errors (imports nothing from the project)
internal/service         business rules; defines the interfaces it needs (future "ports")
internal/repository      SQL / data access (future driven adapters)
internal/transport/http  routing, middleware, JSON (future driving adapter)
internal/config          environment loading
internal/platform/...    cross-cutting infra (cache, queue, storage clients) — added per milestone
```

**Dependency rule (enforced in review):**
- `transport → service → domain`, `repository → domain`, `cmd/api → everything`.
- `service` **never** imports `repository`, `transport`, `pgx` or `net/http`. It declares small interfaces (consumer-side) that repositories satisfy.
- `domain` imports no project packages and no third-party packages.
- `transport` never touches SQL or `pgx`.

Moving to hexagonal later should be a rename (`service` interfaces → `ports`, `repository`/`transport` → `adapters`), not a rewrite. Do not introduce a `ports` package early.

### Request flow
`router → middleware (request id, recover, metrics, log, CORS, auth) → handler → service → repository → Postgres`

---

## 3. Coding Standards

### Go principles
- **Accept interfaces, return structs.** Interfaces are defined where they are *used*, kept small (1–3 methods).
- **`context.Context` is the first parameter** of every function that does I/O. Never store a context in a struct.
- **Errors are values.** Wrap with `fmt.Errorf("load plan: %w", err)`; compare with `errors.Is/As`. Domain sentinel errors (`domain.ErrNotFound`) are mapped to HTTP status **only** in `transport`.
- **No `panic`** outside `main` startup. No `init()` side effects. No package-level mutable state (Prometheus collectors are the single exception).
- **No global singletons**; dependencies are passed via constructors (`NewXService(store)`).
- **Concurrency:** every goroutine has an owner, a cancellation path (context) and an error path. Shutdown must drain.
- **Types over `any`:** avoid `any`/`interface{}` except at true boundaries (JSON encode, logging). Every request/response body has a struct.

### NASA Power-of-10 adaptation (carried over from the frontend)
1. Simple control flow — early returns over nesting; no deep conditional pyramids.
2. Functions do one thing; keep them **under ~60 lines**.
3. Data scope isolation — services own their state; handlers never reach into repositories.
4. Strict types — no untyped maps for domain data; no ignored errors (`_ = err` needs a comment why).
5. Strict checks — code must pass `gofmt`, `go vet` and `go test -race` cleanly.

### Naming & layout
- Package names are short, lowercase, singular, no underscores. File per concept: `profile_service.go`, `profile_repository.go`.
- Exported identifiers carry a doc comment that starts with the name. Comments explain *why*, not *what*.
- JSON field names are `snake_case` to match the database and the existing frontend types.

### New service template
```go
type PlanStore interface {
    GetByID(ctx context.Context, id string) (domain.Planning, error)
}

type PlanService struct{ store PlanStore }

func NewPlanService(store PlanStore) *PlanService { return &PlanService{store: store} }

func (s *PlanService) Get(ctx context.Context, userID, planID string) (domain.Planning, error) {
    p, err := s.store.GetByID(ctx, planID)
    if err != nil {
        return domain.Planning{}, fmt.Errorf("get plan: %w", err)
    }
    if !p.VisibleTo(userID) { // authorization lives here, not in SQL or transport
        return domain.Planning{}, domain.ErrNotFound
    }
    return p, nil
}
```

### Database access
- Plain parameterised SQL (`$1`, `$2`). **Never** build SQL by string concatenation or `fmt.Sprintf` with user input.
- Cast UUID/enum columns to `::text` when scanning into `string`.
- Queries live in repositories as `const` strings next to the method that uses them.
- Use transactions for multi-statement writes; keep them short.
- Always set timeouts via `context.WithTimeout` for outbound I/O.

### Expand/Contract
Same policy as the frontend: additive schema changes first, old shape still served, contract later. Never couple a deploy of this service to a destructive migration.

---

## 4. Security Notes

- **Authorization is this service's job.** The DB role bypasses RLS. Every read/write must filter by the authenticated `sub` and, where relevant, tenant — and must be covered by a test that proves another user cannot read it.
- **JWT validation:** enforce algorithm allow-list, `exp` required, `aud=authenticated`. Never accept `alg: none`. Never log tokens.
- **Secrets** come from environment only; `.env` is git-ignored. Never commit real keys; `.env.example` holds placeholders.
- **CORS** is an allow-list of the frontend origin, never `*` when credentials/Authorization are used.
- **Input:** bound request body size, validate and normalise at the transport edge, reject unknown fields on write endpoints.
- **Errors to clients are generic** (`"internal error"`); details go to logs with the request id.
- `/metrics` and `/readyz` must not be exposed on the public listener in production.

---

## 5. Testing

- Unit-test **services** with in-memory fakes of their store interfaces (table-driven tests).
- Test **transport** with `httptest` against the real router; assert status codes and JSON shape.
- Test **repositories** against a real Postgres (compose/testcontainers), not mocks.
- Run with `-race`. New behaviour ships with a test; a bug fix ships with a regression test.

---

## 6. Observability Conventions

- Logs: structured `slog` JSON, always include `request_id`; no PII or tokens.
- Metrics: label by **route pattern**, not raw path (bounded cardinality). Names are `snake_case` with unit suffix (`_seconds`, `_total`).
- Every new external dependency (Redis, queue, storage) ships with: a latency histogram, an error counter and a readiness check.

---

## 7. Workflow

**Branches:** `main` ← PR from `dev` ← PR from `feat/*`, `fix/*`.

**Commits:** `<type>(<scope>): <description>`
Types: `feat, fix, refactor, docs, test, chore, perf, ci`.
Scopes: `auth, plan, workout, cache, queue, storage, metrics, config, infra, deps`.

Commit attribution rules are in `.clouderule` — never add Claude/Anthropic co-author or "Generated with" lines.

**PR checklist:**
- [ ] `gofmt`, `go vet`, `go test -race ./...` pass
- [ ] New endpoints enforce authentication **and** authorization with a test
- [ ] No schema change made in this repo
- [ ] New config documented in `.env.example` and `config.go`
- [ ] New dependency justified; no dead code left behind
- [ ] Metrics/logs added for new I/O paths
