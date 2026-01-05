---
name: go-patterns
description: "Go patterns for BoostedBudget: Huma operations, service layer, repository patterns, error handling, and testing."
version: "1.0.0"
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
