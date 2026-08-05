---
name: go-patterns
description: "Go patterns for BoostedBudget: Huma operations, service layer, repository patterns, error handling, and testing."
---

# Go Patterns Guide

## Architecture Overview

```
cmd/server/main.go          # Entry point, dependency wiring
internal/
├── huma/
│   ├── operations/         # HTTP handlers (Huma operations)
│   ├── inputs/             # Request input types with resolvers
│   └── errors/             # Error mapping
├── services/               # Business logic
├── repository/             # Data access
├── domain/                 # Domain models
├── middleware/             # HTTP middleware
├── workers/                # Background jobs
└── auth/                   # Auth utilities
```

## Huma Operation Pattern

### Handler Structure

```go
// internal/huma/operations/features.go
package operations

import (
    "context"
    "net/http"

    "github.com/danielgtaylor/huma/v2"
)

type FeatureHandler struct {
    service services.FeatureService
}

func NewFeatureHandler(service services.FeatureService) *FeatureHandler {
    return &FeatureHandler{service: service}
}
```

### Input Types

Always embed `AuthenticatedInput` for protected endpoints:

```go
type GetFeaturesInput struct {
    inputs.AuthenticatedInput  // Provides UserID from JWT
    inputs.PaginationInput     // Provides Limit, Offset

    // Path parameters
    ID string `path:"id" doc:"Feature ID" example:"123e4567-e89b-12d3-a456-426614174000"`

    // Query parameters
    Status string `query:"status" doc:"Filter by status" enum:"active,inactive"`
    Search string `query:"search" doc:"Search term"`
}
```

### Output Types

```go
type GetFeaturesOutput struct {
    Body *FeatureListResponse
}

type FeatureListResponse struct {
    Features []domain.Feature `json:"features"`
    Total    int              `json:"total"`
    Limit    int              `json:"limit"`
    Offset   int              `json:"offset"`
}
```

### Operation Method

```go
func (h *FeatureHandler) GetFeatures(ctx context.Context, input *GetFeaturesInput) (*GetFeaturesOutput, error) {
    features, total, err := h.service.GetByUserID(ctx, input.UserID, input.Limit, input.Offset)
    if err != nil {
        return nil, err  // Error mapper handles conversion
    }

    return &GetFeaturesOutput{
        Body: &FeatureListResponse{
            Features: features,
            Total:    total,
            Limit:    input.Limit,
            Offset:   input.Offset,
        },
    }, nil
}
```

### Route Registration

```go
func RegisterFeatureRoutes(api huma.API, handler *FeatureHandler) {
    huma.Register(api, huma.Operation{
        OperationID: "getFeatures",
        Method:      http.MethodGet,
        Path:        "/api/v1/features",
        Summary:     "List user features",
        Description: "Returns paginated list of features for the authenticated user",
        Tags:        []string{"Features"},
    }, handler.GetFeatures)

    huma.Register(api, huma.Operation{
        OperationID: "createFeature",
        Method:      http.MethodPost,
        Path:        "/api/v1/features",
        Summary:     "Create feature",
        Tags:        []string{"Features"},
    }, handler.CreateFeature)
}
```

## Service Layer Pattern

### Interface Definition

```go
// internal/services/feature_service.go
package services

type FeatureService interface {
    GetByUserID(ctx context.Context, userID string, limit, offset int) ([]domain.Feature, int, error)
    GetByID(ctx context.Context, userID, featureID string) (*domain.Feature, error)
    Create(ctx context.Context, userID string, req *CreateFeatureRequest) (*domain.Feature, error)
    Update(ctx context.Context, userID, featureID string, req *UpdateFeatureRequest) (*domain.Feature, error)
    Delete(ctx context.Context, userID, featureID string) error
}
```

### Implementation

```go
type featureService struct {
    repo   repository.FeatureRepository
    config *config.Config
}

func NewFeatureService(repo repository.FeatureRepository, config *config.Config) FeatureService {
    return &featureService{
        repo:   repo,
        config: config,
    }
}

func (s *featureService) GetByUserID(ctx context.Context, userID string, limit, offset int) ([]domain.Feature, int, error) {
    features, err := s.repo.GetByUserID(ctx, userID, limit, offset)
    if err != nil {
        return nil, 0, fmt.Errorf("failed to get features: %w", err)
    }

    total, err := s.repo.CountByUserID(ctx, userID)
    if err != nil {
        return nil, 0, fmt.Errorf("failed to count features: %w", err)
    }

    return features, total, nil
}
```

## Repository Pattern

### Interface

```go
// internal/repository/feature_repository.go
package repository

type FeatureRepository interface {
    GetByUserID(ctx context.Context, userID string, limit, offset int) ([]domain.Feature, error)
    GetByID(ctx context.Context, userID, featureID string) (*domain.Feature, error)
    Create(ctx context.Context, feature *domain.Feature) error
    Update(ctx context.Context, feature *domain.Feature) error
    Delete(ctx context.Context, userID, featureID string) error
    CountByUserID(ctx context.Context, userID string) (int, error)
}
```

### Implementation

```go
type featureRepository struct {
    db *sql.DB
}

func NewFeatureRepository(db *sql.DB) FeatureRepository {
    return &featureRepository{db: db}
}

func (r *featureRepository) GetByUserID(ctx context.Context, userID string, limit, offset int) ([]domain.Feature, error) {
    query := `
        SELECT id, user_id, name, status, created_at, updated_at
        FROM features
        WHERE user_id = $1
        ORDER BY created_at DESC
        LIMIT $2 OFFSET $3
    `

    rows, err := r.db.QueryContext(ctx, query, userID, limit, offset)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var features []domain.Feature
    for rows.Next() {
        var f domain.Feature
        if err := rows.Scan(&f.ID, &f.UserID, &f.Name, &f.Status, &f.CreatedAt, &f.UpdatedAt); err != nil {
            return nil, err
        }
        features = append(features, f)
    }

    return features, rows.Err()
}
```

### Nullable columns: never scan SQL `NULL` into a non-pointer field (Issue #1444)

**This is a convention, not an enforced lint rule** — the repo has no `.golangci.yml`/`.yaml`
anywhere, so `golangci-lint run` uses its stock defaults only, and none of those defaults catch
this.

**Failure mode:** scanning a nullable SQL column directly into a non-pointer Go field (e.g.
`&f.Name` where `Name string` and the column allows `NULL`) panics or errors the moment a real
`NULL` row is read — pgx (and `database/sql`) cannot convert SQL `NULL` into a non-pointer string,
int, etc. This is a repo-wide risk anywhere a `SELECT` scans a nullable column straight into a
struct field typed as the bare Go type rather than a pointer or null-aware wrapper.

**Three remedies, pick whichever fits the column:**

1. **`COALESCE` in the SQL** — `SELECT COALESCE(status, 'unknown') FROM ...` — cheapest when a
   sensible non-NULL default exists and the column's NULL-ness is incidental, not meaningful.
2. **A nullable intermediate** — scan into `*string` or `sql.NullString` (or the `*int`/`sql.Null*`
   equivalent), then convert to the domain type with the actual NULL-vs-value branching made
   explicit in Go, when NULL carries real meaning the caller needs to distinguish from a value.
3. **A `NOT NULL` migration** — if the column is never supposed to be NULL by design, fix the
   schema instead of coding around it (see the three-step `NOT NULL` migration pattern in
   `db-migration-guide`'s Grace-Window Timestamps section) so the scan target's non-pointer type is
   actually guaranteed correct rather than assumed.

**The trap that hides this bug from you:** a `WHERE status NOT IN ('a', 'b')` or `WHERE status <>
'x'` predicate on a nullable column evaluates to `UNKNOWN` (not `TRUE`) for a `NULL` row in SQL's
three-valued logic, so those rows are silently excluded from the result set. That means a query
guarded by exactly this kind of predicate can scan cleanly in production for a long time — not
because no row is ever NULL, but because the predicate itself is quietly filtering the NULL rows
out before they ever reach the non-pointer scan. The panic surfaces only once a query without that
filter (a new caller, a broadened `WHERE`, an admin report) reads one of those rows directly.

**Testing consequence:** a RED test for this must **seed the NULL via raw SQL** (a direct `INSERT`
or `UPDATE ... SET col = NULL`), not through the normal fixture/factory path. If no production
writer can actually produce a NULL in that column, a fixture-built test will pass before the fix is
even applied — proving nothing about the real hazard. The test only has teeth if it reproduces the
same on-disk state a real NULL row would leave, independent of whether the application's own
writers currently know how to create one.

### Keyset pagination: a tie-breaker no test ever ties on is indistinguishable from an absent one (#1478/#1469)

Every bounded sweep in this repo pages with a keyset cursor rather than `OFFSET`, and every one of
them needs a **total** order — a unique final column, in practice `id`:

```sql
ORDER BY sort_col ASC, id ASC
WHERE (sort_col, id) > ($1, $2)      -- tuple must match the ORDER BY exactly
```

Two rules that are easy to satisfy in the SQL and easy to leave unverified in the tests:

**1. Seed a tie, or you have not tested the tie-breaker.** Three separate paged sweeps on this repo
shipped with an `id` tie-breaker that **no test could detect the removal of** — deleting `id` from
both the `ORDER BY` and the cursor tuple left the whole suite green. The cause was identical each
time: the fixture helper seeded strictly increasing timestamps (`epoch.Add(i * time.Minute)`), so a
tie never existed to disambiguate. Seed **N rows sharing one `sort_col` value** and assert each row
is returned **exactly once** across pages. Without a tie in the fixture, the tie-breaker is decoration.

**2. Assert the cursor the *worker* builds, not just the one the test builds.** The same three sweeps
had tests that constructed cursors themselves and passed them to the repository — so nothing
connected the worker's cursor-construction code to the query. A worker that built
`(time.Time{}, lastID)` instead of the real timestamp passed every test, while in production the
tuple's first element decides every comparison, the query returns the **head page forever**, and any
row whose per-row action fails sits at the head of every page and consumes the entire sweep budget.
That is the exact unbounded-scan starvation the cap was added to prevent, reintroduced through the
cursor. Assert the timestamp component of the cursor the worker produces.

**Nullable sort columns:** a row-value comparison `(a, b) > (x, y)` takes **no per-column
`NULLS FIRST/LAST` clause**, so it cannot express `ORDER BY a ASC NULLS LAST` directly. Wrap both
sides in the same total-order expression — `COALESCE(col, 'infinity'::timestamptz)` reproduces
`NULLS LAST` under `ASC` — and keep one shared constant for the sort key so the `ORDER BY` and the
cursor cannot drift apart. These `COALESCE` wrappers look redundant; they are load-bearing.

**Cap prose:** every sweep here restarts from a nil cursor, so a per-run cap does **not** give you
"no starvation" — above the cap the oldest rows block everything behind them **permanently** until
the backlog drains. State the guarantee bounded by the cap; an unqualified "no starvation" claim is
false. And a per-run cap is not a per-day ceiling: River's `ByPeriod` rounds inserts into **fixed
buckets**, not a rolling window, so two runs either side of a boundary can execute close together.

## Domain Model Pattern

```go
// internal/domain/feature.go
package domain

import (
    "errors"
    "time"
)

var (
    ErrFeatureNotFound      = errors.New("feature not found")
    ErrFeatureAlreadyExists = errors.New("feature already exists")
    ErrFeatureLimitExceeded = errors.New("feature limit exceeded")
)

type Feature struct {
    ID        string    `json:"id" db:"id"`
    UserID    string    `json:"user_id" db:"user_id"`
    Name      string    `json:"name" db:"name"`
    Status    string    `json:"status" db:"status"`
    CreatedAt time.Time `json:"created_at" db:"created_at"`
    UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
}

func NewFeature(userID, name string) *Feature {
    now := time.Now()
    return &Feature{
        UserID:    userID,
        Name:      name,
        Status:    "active",
        CreatedAt: now,
        UpdatedAt: now,
    }
}
```

## Error Handling

### Error Mapping

```go
// internal/huma/errors/mapper.go
package errors

func MapError(err error) error {
    switch {
    case errors.Is(err, domain.ErrFeatureNotFound):
        return huma.Error404NotFound("Feature not found")
    case errors.Is(err, domain.ErrFeatureAlreadyExists):
        return huma.Error409Conflict("Feature already exists")
    case errors.Is(err, domain.ErrFeatureLimitExceeded):
        return huma.Error403Forbidden("Feature limit exceeded for your subscription tier")
    case errors.Is(err, services.ErrInvalidInput):
        return huma.Error400BadRequest("Invalid input")
    default:
        // Log unexpected error
        log.Printf("Unexpected error: %v", err)
        return huma.Error500InternalServerError("Internal server error")
    }
}
```

## Testing Pattern

### Mock Repository

```go
// internal/services/feature_service_test.go
package services

import (
    "context"
    "testing"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/mock"
)

type MockFeatureRepository struct {
    mock.Mock
}

func (m *MockFeatureRepository) GetByUserID(ctx context.Context, userID string, limit, offset int) ([]domain.Feature, error) {
    args := m.Called(ctx, userID, limit, offset)
    if args.Get(0) == nil {
        return nil, args.Error(1)
    }
    return args.Get(0).([]domain.Feature), args.Error(1)
}

// Implement other interface methods...
```

### Service Test

```go
func TestFeatureService_GetByUserID(t *testing.T) {
    mockRepo := new(MockFeatureRepository)
    service := NewFeatureService(mockRepo, nil)

    ctx := context.Background()
    userID := "user-123"
    expectedFeatures := []domain.Feature{
        {ID: "1", UserID: userID, Name: "Feature 1"},
    }

    mockRepo.On("GetByUserID", ctx, userID, 50, 0).Return(expectedFeatures, nil)
    mockRepo.On("CountByUserID", ctx, userID).Return(1, nil)

    features, total, err := service.GetByUserID(ctx, userID, 50, 0)

    assert.NoError(t, err)
    assert.Equal(t, expectedFeatures, features)
    assert.Equal(t, 1, total)
    mockRepo.AssertExpectations(t)
}
```

## Worker Pattern

### Background Job

```go
// internal/workers/feature_processor.go
package workers

type FeatureProcessor struct {
    service services.FeatureService
    logger  *log.Logger
}

func NewFeatureProcessor(service services.FeatureService) *FeatureProcessor {
    return &FeatureProcessor{
        service: service,
        logger:  log.Default(),
    }
}

func (p *FeatureProcessor) Start(ctx context.Context) {
    ticker := time.NewTicker(30 * time.Second)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            p.logger.Println("Feature processor stopped")
            return
        case <-ticker.C:
            if err := p.process(ctx); err != nil {
                p.logger.Printf("Error processing features: %v", err)
            }
        }
    }
}

func (p *FeatureProcessor) process(ctx context.Context) error {
    // Process batch of features
    return nil
}
```

## Wiring in main.go

```go
// cmd/server/main.go

// Create repositories
featureRepo := repository.NewFeatureRepository(db)

// Create services
featureService := services.NewFeatureService(featureRepo, cfg)

// Create handlers
featureHandler := operations.NewFeatureHandler(featureService)

// Register routes
operations.RegisterFeatureRoutes(humaAPI, featureHandler)

// Start workers
go featureProcessor.Start(workerCtx)
```

## Quick Reference

| Layer | Location | Responsibility |
|-------|----------|----------------|
| Handler | `internal/huma/operations/` | HTTP handling, I/O transformation |
| Service | `internal/services/` | Business logic |
| Repository | `internal/repository/` | Data access |
| Domain | `internal/domain/` | Models, errors |
| Middleware | `internal/middleware/` | Cross-cutting concerns |
| Workers | `internal/workers/` | Background jobs |
