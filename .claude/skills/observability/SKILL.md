---
name: observability
description: "Observability guide for BoostedBudget: logging with slog, Sentry error tracking, OpenTelemetry tracing, Prometheus metrics, context propagation, and frontend error boundaries. Use when adding new services/workers, adding new error types to MapError, adding Prometheus metrics, debugging logging or tracing issues, or wiring observability into new code."
---

# Observability Guide

BoostedBudget uses slog (unified logging), Sentry (error tracking), OpenTelemetry (tracing), Prometheus (metrics), and React error boundaries with Sentry.

---

## Logging

### Logger Injection Pattern

Follow the gold standard from `backend/internal/queue/jobs/plaid_sync.go`:

```go
// 1. Deps struct
type MyWorkerDeps struct {
    Logger *slog.Logger
    // other deps...
}

// 2. Constructor with nil fallback
func NewMyWorker(deps MyWorkerDeps) *MyWorker {
    logger := deps.Logger
    if logger == nil {
        logger = slog.Default()
    }
    return &MyWorker{logger: logger}
}

// 3. Usage
w.logger.Info("processing item", "item_id", id, "user_id", userID)
w.logger.Error("failed to process", "error", err, "item_id", id)
```

Never use `log.Printf`. Always use `*slog.Logger`.

### Context-Aware Logging

Use `observability.LoggerFromContext(ctx)` when a logger isn't in the struct (e.g., in standalone functions):

```go
import "github.com/maxnussbaum/BoostedBudget/internal/observability"

func ProcessSomething(ctx context.Context, id string) error {
    logger := observability.LoggerFromContext(ctx)
    logger.InfoContext(ctx, "processing", "id", id)
    // ...
}
```

`LoggerFromContext` enriches the default logger with `request_id` and `user_id` from context.

### Log Levels

| Level | When to use |
|-------|-------------|
| `Debug` | Verbose detail (query params, intermediate values) |
| `Info` | Normal flow milestones (job started, item synced) |
| `Warn` | Recoverable issues, optional config missing |
| `Error` | Failures requiring attention (always include `"error", err`) |

---

## Error Handling

### MapError — Domain → HTTP

Add new sentinel errors to `backend/internal/huma/errors/mapper.go`:

```go
// In the appropriate section of MapError:
case errors.Is(err, domain.ErrMyNewError):
    return huma.Error404NotFound("My resource not found")
```

Sections: 404 Not Found → 409 Conflict → 403 Forbidden → 429 Too Many Requests → 400 Bad Request → 503 Service Unavailable → 500 Default.

### LoggingMapError — Use in Handlers

Replace `humaerrors.MapError(err)` with `humaerrors.LoggingMapError(ctx, err)` in all operation handlers:

```go
func (h *MyHandler) Handle(ctx context.Context, input *MyInput) (*MyOutput, error) {
    result, err := h.service.DoThing(ctx, input.ID)
    if err != nil {
        return nil, humaerrors.LoggingMapError(ctx, err)
    }
    return &MyOutput{Body: result}, nil
}
```

`LoggingMapError` automatically:
- Maps the error to the correct HTTP status via `MapError`
- Logs the original error via slog for 5xx responses
- Captures 5xx errors in Sentry

### Error Code Constants

Use constants from `backend/internal/huma/errors/codes.go` in error responses:

```go
const (
    CodeNotFound     = "RESOURCE_NOT_FOUND"
    CodeConflict     = "RESOURCE_ALREADY_EXISTS"
    CodeValidation   = "VALIDATION_ERROR"
    CodeTierLimit    = "TIER_LIMIT_EXCEEDED"
    CodeRateLimit    = "RATE_LIMIT_EXCEEDED"
    CodeServiceDown  = "EXTERNAL_SERVICE_UNAVAILABLE"
    CodeUnauthorized = "UNAUTHORIZED"
    CodeInternal     = "INTERNAL_ERROR"
)
```

---

## Context Propagation

Use `backend/internal/observability/context.go` to carry request_id and user_id through the call stack:

```go
// Store (done by middleware automatically)
ctx = observability.WithRequestID(ctx, requestID)
ctx = observability.WithUserID(ctx, userID)

// Retrieve
requestID := observability.RequestIDFromContext(ctx)
userID    := observability.UserIDFromContext(ctx)
```

The `RequestID` middleware automatically stores request ID in `context.Context`. The Clerk middleware stores user ID. Never pass these as function parameters — use context.

---

## Sentry

### Initialization

Configured in `backend/cmd/server/main.go` via `observability.InitSentry()`. Enabled only when `SENTRY_DSN` env var is set. No-op otherwise.

### Manual Capture

For errors that aren't caught by `LoggingMapError`:

```go
import "github.com/getsentry/sentry-go"

sentry.CaptureException(err)

// With context (preferred):
hub := sentry.GetHubFromContext(ctx)
if hub != nil {
    hub.CaptureException(err)
}
```

`sentryecho` middleware handles panic recovery automatically (registered in main.go).

---

## OpenTelemetry

Configured in `backend/cmd/server/main.go` via `observability.InitTracer()`. Enabled only when `OTEL_EXPORTER_OTLP_ENDPOINT` env var is set. No-op otherwise.

`otelecho` middleware automatically creates spans for each HTTP request. The `TracingHandler` slog handler adds `trace_id` and `span_id` to log records automatically when a span is active.

For DB query tracing, `otelpgx` instruments pgx automatically (wired in `database` package if configured).

---

## Prometheus Metrics

### Application Metrics (backend/internal/observability/metrics.go)

```go
// Error counter (incremented by LoggingMapError for 5xx)
observability.AppErrorsTotal.WithLabelValues(errorType, statusCode).Inc()

// Auth (incremented by clerk middleware)
observability.AuthAttempts.WithLabelValues("success" | "invalid" | "expired").Inc()
observability.AuthRegistrations.Inc()

// Plaid sync (incremented by PlaidSyncWorker)
observability.PlaidSyncDuration.Observe(duration.Seconds())
observability.PlaidSyncTxns.WithLabelValues("added" | "modified" | "removed").Add(float64(n))

// River jobs — increment in worker Work() methods:
observability.JobsCompleted.WithLabelValues(kind, queue, "success" | "failed").Inc()
observability.JobDuration.WithLabelValues(kind).Observe(duration.Seconds())

// Dead letter queue size — update in system health service
observability.DeadLetterQueueSize.Set(float64(count))
```

### Adding New Metrics

Add to `backend/internal/observability/metrics.go` using `promauto`:

```go
var MyNewCounter = promauto.NewCounterVec(prometheus.CounterOpts{
    Name: "my_feature_events_total",
    Help: "Total number of my feature events by type.",
}, []string{"event_type"})
```

Naming convention: `<subsystem>_<name>_<unit>` (e.g., `plaid_sync_duration_seconds`, `auth_attempts_total`).

### HTTP Metrics

Existing HTTP metrics (request count, latency, status codes) are handled by `backend/internal/middleware/metrics.go` — no changes needed there.

### DB Pool Metrics

`observability.StartDBMetricsCollector(ctx, pool)` runs in main.go and exposes `db_pool_*` gauges automatically. No instrumentation needed in repositories.

---

## Frontend Error Handling

### Sentry Init

`initSentry()` is called in `apps/web/src/main.tsx`. Reads `window.ENV.SENTRY_DSN`. No-op if unset.

### Error Boundaries

Use `FeatureErrorBoundary` to wrap feature routes in `App.tsx`:

```tsx
import { FeatureErrorBoundary } from './components/FeatureErrorBoundary'

<FeatureErrorBoundary featureName="budgets">
  <BudgetsPage />
</FeatureErrorBoundary>
```

Use `ErrorBoundary` for app-level recovery at the root.

### API Error Parsing

```typescript
import { parseApiError, useApiErrorHandler } from '@/shared/hooks/useApiError'

// In components:
const { handleError } = useApiErrorHandler()

try {
  await createBudget(data)
} catch (err) {
  const apiError = handleError(err)
  setErrorMessage(apiError.detail)
}
```

`parseApiError` reads RFC 9457 response bodies: `{ status, title, detail, code, requestId }`.

`useApiErrorHandler` captures 5xx errors to Sentry automatically.

---

## Key Files

| File | Purpose |
|------|---------|
| `backend/internal/observability/` | Logger factory, Sentry init, OTEL init, context helpers, TracingHandler, Prometheus metrics, DB pool metrics |
| `backend/internal/middleware/logging.go` | slog-based HTTP request logger (StructuredLogging middleware) |
| `backend/internal/middleware/recovery.go` | Panic recovery: RFC 9457 response + slog + app_panics_total |
| `backend/internal/huma/errors/mapper.go` | MapError + LoggingMapError |
| `backend/internal/huma/errors/codes.go` | Error code string constants |
| `apps/web/src/shared/lib/sentry.ts` | Frontend Sentry init (no-op without DSN) |
| `apps/web/src/components/ErrorBoundary.tsx` | App-level error boundary with Sentry |
| `apps/web/src/components/FeatureErrorBoundary.tsx` | Per-feature error boundary with reset |
| `apps/web/src/shared/hooks/useApiError.ts` | RFC 9457 parser + Sentry capture hook |

---

## Checklist: Adding a New Service or Worker

- [ ] Add `logger *slog.Logger` to struct and Deps
- [ ] Nil-check in constructor: `if logger == nil { logger = slog.Default() }`
- [ ] Use `w.logger.InfoContext(ctx, ...)` (not `log.Printf`)
- [ ] Pass `appLogger` (not `slog.New(...)`) when constructing in `main.go`

## Checklist: Adding a New Error Type

- [ ] Define sentinel error in domain/repository/services package
- [ ] Add `errors.Is(err, MyErr)` case to `MapError` in `mapper.go`
- [ ] Use `LoggingMapError(ctx, err)` in the operation handler

## Checklist: Adding New Metrics

- [ ] Add counter/histogram/gauge to `observability/metrics.go` via `promauto`
- [ ] Instrument the relevant code path
- [ ] Verify metric appears at `/metrics` endpoint
