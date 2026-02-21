---
name: system-health-updater
description: >
  Guide developers through updating the system health dashboard when adding new systems, services, or job queues to BoostedBudget.
  Use when adding new River job queues, integrating external services (Stripe, SendGrid), adding background workers,
  creating infrastructure that needs monitoring, or when user mentions health dashboard with new service/queue/system.
---

# System Health Updater

Guide developers to update the admin health dashboard (`/admin/health`) when new systems, services, or job queues are added to ensure proper monitoring.

## When to Use

Trigger this skill when:
- Adding new River job queues (email, reports, exports, etc.)
- Integrating new external services (Stripe, SendGrid, Twilio)
- Adding new background workers or cron jobs
- Creating database tables that need health monitoring
- User asks about monitoring new infrastructure

## Update Checklist

### Backend Updates

1. **Domain Models** (`backend/internal/domain/system_health.go`)
   - Add new fields to relevant structs (e.g., `ServiceHealthStatuses` for services)
   - Update JSON tags
   - Keep field names snake_case for JSON serialization

2. **Repository** (`backend/internal/repository/system_health_repository.go`)
   - Add new query methods for new metrics
   - Use indexed columns in WHERE clauses
   - Test queries return expected data types
   - Example:
     ```go
     func (r *SystemHealthRepository) GetEmailQueueStats(ctx context.Context) (*domain.QueueMetrics, error) {
         query := `
             SELECT
                 COUNT(*) FILTER (WHERE state='available') as pending,
                 COUNT(*) FILTER (WHERE state='running') as running,
                 COUNT(*) FILTER (WHERE state IN ('retryable','discarded')) as failed
             FROM river_job WHERE queue='email'
         `
         // ...
     }
     ```

3. **Service** (`backend/internal/services/system_health_service.go`)
   - Add new check methods (e.g., `checkStripe()`, `checkSendGrid()`)
   - Update `GetSystemHealth()` to call new checks
   - Update `calculateOverallStatus()` if new metrics affect health logic
   - For new queues, add to queues slice:
     ```go
     queues := []string{"critical", "default", "notifications", "low", "email"}
     ```

4. **Tests**
   - Add unit tests for new repository methods (mock DB)
   - Add service tests for new metrics (mock repositories)
   - Update integration tests with realistic test data

5. **OpenAPI Generation**
   - Run `make generate-openapi` after backend changes
   - Verify new fields appear in `api/openapi.yaml`
   - Run `make generate-api-client` to update TypeScript types

### Frontend Updates

1. **Component** (`apps/web/src/features/admin/components/HealthDashboardPage.tsx`)
   - Add new `SummaryCard` for service status
   - Add new section if grouping multiple metrics
   - Update rendering logic for new data
   - Example:
     ```tsx
     <SummaryCard
       title="Stripe"
       value={data.services.stripe.status}
       icon={<CreditCard size={24} />}
       variant={statusToVariant(data.services.stripe.status)}
       subtitle={`${data.services.stripe.latency_ms}ms latency`}
     />
     ```

2. **Mock Data** (`apps/web/src/test/mocks/admin-api.ts`)
   - Add new fields to `mockHealthData` object
   - Provide realistic test values
   - Ensure types match generated API client

3. **Tests**
   - Update component tests for new metrics
   - Test new cards render correctly
   - Verify status color mapping (success/warning/error)
   - Test auto-refresh updates new data

### Verification Steps

1. Start backend: `make dev`
2. Verify API response includes new metrics:
   ```bash
   curl -H "Authorization: Bearer $ADMIN_TOKEN" \
        http://localhost:8080/api/v1/admin/system/health | jq
   ```
3. Start frontend: `cd apps/web && bun run dev`
4. Navigate to `/admin/health` (requires admin user)
5. Verify new metrics display correctly
6. Test auto-refresh updates (wait 30s)
7. Run all tests: `make test-all`

## Example: Adding Email Queue

**Scenario**: New `email` queue for sending notification emails

### Backend Changes

**Service** (`system_health_service.go`):
```go
func (s *systemHealthService) GetSystemHealth(ctx context.Context) (*domain.SystemHealthResponse, error) {
    // Add "email" to queues list
    queues := []string{"critical", "default", "notifications", "low", "email"}
    // ... rest unchanged
}
```

### Frontend Changes

No changes needed - `QueueCard` auto-renders from API response.

### Tests

**Component test** (`HealthDashboardPage.test.tsx`):
```typescript
it('should render email queue card', () => {
  const dataWithEmail = {
    ...mockHealthData,
    job_queues: {
      ...mockHealthData.job_queues,
      email: { pending: 10, running: 2, failed: 0, completed_last_hour: 50 }
    }
  }
  render(<HealthDashboardPage />, { mockData: dataWithEmail })
  expect(screen.getByText('email')).toBeInTheDocument()
})
```

## Example: Adding Stripe Service

**Scenario**: Monitor Stripe API availability and latency

### Backend Changes

**Domain** (`system_health.go`):
```go
type ServiceHealthStatuses struct {
    Database DatabaseHealth `json:"database"`
    Clerk    ServiceHealth  `json:"clerk"`
    Plaid    PlaidHealth    `json:"plaid"`
    Stripe   ServiceHealth  `json:"stripe"`  // NEW
}
```

**Service** (`system_health_service.go`):
```go
func (s *systemHealthService) checkStripe(ctx context.Context) domain.ServiceHealth {
    start := time.Now()
    // Ping Stripe API (e.g., retrieve account info)
    _, err := s.stripeClient.Account.GetByID(ctx, "acct_123")
    latency := time.Since(start).Milliseconds()

    if err != nil {
        return domain.ServiceHealth{Status: "down", LatencyMs: latency}
    }
    return domain.ServiceHealth{Status: "up", LatencyMs: latency}
}

func (s *systemHealthService) GetSystemHealth(ctx context.Context) (*domain.SystemHealthResponse, error) {
    // ...
    response.Services = domain.ServiceHealthStatuses{
        Database: s.checkDatabase(ctx),
        Clerk:    s.checkClerk(ctx),
        Plaid:    s.checkPlaid(ctx),
        Stripe:   s.checkStripe(ctx),  // NEW
    }
    // ...
}
```

### Frontend Changes

**Component** (`HealthDashboardPage.tsx`):
```tsx
<SummaryCard
  title="Stripe"
  value={data.services.stripe.status}
  icon={<CreditCard size={24} />}
  variant={statusToVariant(data.services.stripe.status)}
  subtitle={`${data.services.stripe.latency_ms}ms latency`}
/>
```

**Mock data** (`admin-api.ts`):
```typescript
services: {
  // ... existing services
  stripe: { status: 'up', latency_ms: 85 }
}
```

## Related Files

- **Backend Domain**: `backend/internal/domain/system_health.go`
- **Backend Service**: `backend/internal/services/system_health_service.go`
- **Backend Repository**: `backend/internal/repository/system_health_repository.go`
- **Frontend Page**: `apps/web/src/features/admin/components/HealthDashboardPage.tsx`
- **Frontend Mocks**: `apps/web/src/test/mocks/admin-api.ts`
- **Tests**: `*_test.go`, `*.test.tsx`
