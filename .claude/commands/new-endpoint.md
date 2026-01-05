# New API Endpoint

Scaffold a new Go API endpoint following Huma patterns with full type generation.

## Usage

```
/new-endpoint [resource-name] [operation]
```

Example: `/new-endpoint budgets create`

## Complete Workflow

### 1. Design the API in OpenAPI Spec

Update `api/openapi.yaml` with the new endpoint:

```yaml
paths:
  /api/v1/resources:
    get:
      operationId: getResources
      summary: List resources
      tags: [Resources]
      security:
        - BearerAuth: []
      parameters:
        - $ref: '#/components/parameters/Limit'
        - $ref: '#/components/parameters/Offset'
      responses:
        '200':
          description: Success
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/ResourceListResponse'
```

### 2. Create Handler File

`backend/internal/huma/operations/resources.go`:

```go
package operations

import (
    "context"
    "net/http"

    "github.com/danielgtaylor/huma/v2"
    "github.com/BoostedBudget/backend/internal/huma/inputs"
    "github.com/BoostedBudget/backend/internal/services"
)

type ResourceHandler struct {
    service services.ResourceService
}

func NewResourceHandler(service services.ResourceService) *ResourceHandler {
    return &ResourceHandler{service: service}
}

// Input/Output types
type GetResourcesInput struct {
    inputs.AuthenticatedInput
    inputs.PaginationInput
}

type GetResourcesOutput struct {
    Body *services.ResourceListResponse
}

// Operation method
func (h *ResourceHandler) GetResources(ctx context.Context, input *GetResourcesInput) (*GetResourcesOutput, error) {
    result, err := h.service.GetByUserID(ctx, input.UserID, input.Limit, input.Offset)
    if err != nil {
        return nil, err
    }
    return &GetResourcesOutput{Body: result}, nil
}

// Route registration
func RegisterResourceRoutes(api huma.API, handler *ResourceHandler) {
    huma.Register(api, huma.Operation{
        OperationID: "getResources",
        Method:      http.MethodGet,
        Path:        "/api/v1/resources",
        Summary:     "List resources",
        Tags:        []string{"Resources"},
    }, handler.GetResources)
}
```

### 3. Create Service Interface

`backend/internal/services/resource_service.go`:

```go
package services

import "context"

type ResourceService interface {
    GetByUserID(ctx context.Context, userID string, limit, offset int) (*ResourceListResponse, error)
    Create(ctx context.Context, userID string, req *CreateResourceRequest) (*Resource, error)
}

type resourceService struct {
    repo repository.ResourceRepository
}

func NewResourceService(repo repository.ResourceRepository) ResourceService {
    return &resourceService{repo: repo}
}

// Implement methods...
```

### 4. Create Repository Interface

`backend/internal/repository/resource_repository.go`:

```go
package repository

type ResourceRepository interface {
    GetByUserID(ctx context.Context, userID string, limit, offset int) ([]domain.Resource, error)
    Create(ctx context.Context, resource *domain.Resource) error
}
```

### 5. Add Error Mapping

`backend/internal/huma/errors/mapper.go`:

```go
case errors.Is(err, domain.ErrResourceNotFound):
    return huma.Error404NotFound("Resource not found")
case errors.Is(err, services.ErrResourceLimitExceeded):
    return huma.Error403Forbidden("Resource limit exceeded for your tier")
```

### 6. Wire in main.go

`backend/cmd/server/main.go`:

```go
// Create repository
resourceRepo := repository.NewResourceRepository(db)

// Create service
resourceService := services.NewResourceService(resourceRepo)

// Create handler
resourceHandler := operations.NewResourceHandler(resourceService)

// Register routes
operations.RegisterResourceRoutes(humaAPI, resourceHandler)
```

### 7. Generate TypeScript Types

```bash
# Backend generates OpenAPI spec on startup
# Or manually:
cd backend && go run cmd/server/main.go --generate-spec

# Generate frontend types with orval
cd frontend && pnpm run generate:api
```

This generates:
- `frontend/src/api/generated/endpoints/resources/resources.ts` - React Query hooks
- `frontend/src/api/generated/models/resource*.ts` - TypeScript types

### 8. Use Generated Hooks in Frontend

```typescript
import { useGetResources, useCreateResource } from '@/api/generated/endpoints/resources/resources'

function ResourcesPage() {
  const { data, isLoading } = useGetResources({ limit: 50 })
  const createMutation = useCreateResource()

  // Use data and mutations...
}
```

## Checklist

- [ ] OpenAPI spec updated
- [ ] Handler with Input/Output types
- [ ] Service interface and implementation
- [ ] Repository interface and implementation
- [ ] Error mapping added
- [ ] Wired in main.go
- [ ] `pnpm run generate:api` executed
- [ ] Frontend hooks usable

## Related

- `.claude/skills/go-patterns/` - Go patterns reference
- `.claude/agents/api-designer.md` - API review agent
