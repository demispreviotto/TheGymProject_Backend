# TheGymProject Backend

Go API that sits beside the Angular + Supabase app. Supabase keeps Auth and Postgres;
this service verifies Supabase JWTs and takes over server-side concerns incrementally
(cache, queue/workers, storage, observability).

## Layout (layered, hexagonal-ready)

```
cmd/api                 composition root (wiring only)
internal/domain         entities + sentinel errors, no imports from the project
internal/service        business rules; defines the interfaces it needs (future ports)
internal/repository     SQL / data access (future driven adapters)
internal/transport/http routing, middleware, JSON (future driving adapter)
internal/config         env loading
```

Dependency rule: transport → service → domain; repository → domain. Service never imports repository.

## Run

```
cp .env.example .env
go mod tidy
make run            # or: make up (api + prometheus + grafana)
```

Endpoints: `/healthz`, `/readyz`, `/metrics`, `GET /v1/me` (Bearer Supabase JWT).
