---
name: integration-guides
description: "Integration guides for BoostedBudget: Clerk authentication, Plaid financial data, and Paddle payments."
---

# Integration Guides

## Overview

BoostedBudget integrates with three main external services:

| Service | Purpose | Key Files |
|---------|---------|-----------|
| **Clerk** | Authentication, user management | `middleware/clerk.go`, `auth/claims.go` |
| **Plaid** | Bank connections, transactions | `services/plaid_service.go`, `workers/webhook_processor.go` |
| **Paddle** | Subscriptions, payments | `services/subscription_service.go` |

## Clerk Authentication

### JWT Template Configuration

Create a JWT template named "BoostedBudget" in Clerk dashboard with these claims:

```json
{
  "user_id": "{{user.id}}",
  "email": "{{user.primary_email_address}}",
  "subscription_tier": "{{user.public_metadata.subscription_tier}}",
  "primary_region": "{{user.public_metadata.primary_region}}"
}
```

### Backend Middleware

```go
// internal/middleware/clerk.go

func ClerkAuthMiddleware(clerkClient clerk.Client, userService services.UserService) echo.MiddlewareFunc {
    return func(next echo.HandlerFunc) echo.HandlerFunc {
        return func(c echo.Context) error {
            // Extract bearer token
            token := strings.TrimPrefix(c.Request().Header.Get("Authorization"), "Bearer ")
            if token == "" {
                return echo.NewHTTPError(http.StatusUnauthorized, "Missing authorization token")
            }

            // Verify JWT
            claims, err := clerkClient.VerifyToken(token)
            if err != nil {
                return echo.NewHTTPError(http.StatusUnauthorized, "Invalid token")
            }

            // Resolve internal user ID
            userID, err := userService.GetOrCreateUserID(c.Request().Context(), claims)
            if err != nil {
                return echo.NewHTTPError(http.StatusInternalServerError, "Failed to resolve user")
            }

            // Set context values
            c.Set("user_id", userID)
            c.Set("clerk_user_id", claims.Subject)
            c.Set("claims", claims)

            return next(c)
        }
    }
}
```

### Frontend Auth Setup

```typescript
// src/api/client.ts

import axios from 'axios'

let getAccessToken: () => Promise<string | null> = async () => null

export const setAuthTokenProvider = (provider: () => Promise<string | null>) => {
  getAccessToken = provider
}

const AXIOS_INSTANCE = axios.create({
  baseURL: import.meta.env.VITE_API_URL,
})

export const customInstance = async <T>(config: AxiosRequestConfig): Promise<T> => {
  const token = await getAccessToken()
  if (token) {
    config.headers = {
      ...config.headers,
      Authorization: `Bearer ${token}`,
    }
  }
  const { data } = await AXIOS_INSTANCE(config)
  return data
}

// In main.tsx
import { useAuth } from '@clerk/clerk-react'

function InitializeAuth() {
  const { getToken } = useAuth()

  useEffect(() => {
    setAuthTokenProvider(() => getToken({ template: 'BoostedBudget' }))
  }, [getToken])

  return null
}
```

### Test Mode

For testing, use Clerk's built-in test mode:

| Feature | Value |
|---------|-------|
| Test email format | `any_email+clerk_test@example.com` |
| Verification code | `424242` |
| Test phone numbers | `+1555555XXXX` (100-199) |

## Plaid Financial Data

### Environment Configuration

```bash
# .env
PLAID_CLIENT_ID=your_client_id
PLAID_SECRET=your_secret
PLAID_ENV=sandbox  # sandbox, development, production
```

### Link Token Creation

```go
// internal/services/plaid_service.go

func (s *plaidService) CreateLinkToken(ctx context.Context, userID string) (string, error) {
    user := plaid.LinkTokenCreateRequestUser{
        ClientUserId: userID,
    }

    request := plaid.NewLinkTokenCreateRequest(
        "BoostedBudget",
        "en",
        []plaid.CountryCode{plaid.COUNTRYCODE_US},
        user,
    )
    request.SetProducts([]plaid.Products{plaid.PRODUCTS_TRANSACTIONS})

    resp, _, err := s.client.PlaidApi.LinkTokenCreate(ctx).LinkTokenCreateRequest(*request).Execute()
    if err != nil {
        return "", fmt.Errorf("failed to create link token: %w", err)
    }

    return resp.GetLinkToken(), nil
}
```

### Webhook Processing

```go
// internal/workers/webhook_processor.go

func (p *WebhookProcessor) processTransactionWebhook(ctx context.Context, event *domain.WebhookEvent) error {
    switch event.WebhookCode {
    case "INITIAL_UPDATE":
        // First sync - get historical transactions based on tier
        return p.syncService.InitialSync(ctx, event.ItemID)

    case "DEFAULT_UPDATE":
        // Incremental update - cursor-based
        return p.syncService.IncrementalSync(ctx, event.ItemID)

    case "HISTORICAL_UPDATE":
        // Backfill old transactions
        return p.syncService.HistoricalSync(ctx, event.ItemID)

    case "TRANSACTIONS_REMOVED":
        // Handle retroactive deletions
        return p.syncService.RemoveTransactions(ctx, event.ItemID, event.RemovedTransactionIDs)

    default:
        log.Printf("Unknown webhook code: %s", event.WebhookCode)
        return nil
    }
}
```

### Webhook Codes Reference

| Code | When Triggered | Action |
|------|----------------|--------|
| `INITIAL_UPDATE` | First connection | Sync historical transactions (30/365/730 days by tier) |
| `DEFAULT_UPDATE` | New transactions | Incremental cursor-based sync |
| `HISTORICAL_UPDATE` | Plaid backfills | Sync older transactions |
| `TRANSACTIONS_REMOVED` | Bank removes transactions | Delete from database |

### Access Token Encryption

```go
// internal/services/plaid_service.go

func (s *plaidService) ExchangePublicToken(ctx context.Context, userID, publicToken string) (*domain.LinkedItem, error) {
    // Exchange public token for access token
    resp, _, err := s.client.PlaidApi.ItemPublicTokenExchange(ctx).
        ItemPublicTokenExchangeRequest(*plaid.NewItemPublicTokenExchangeRequest(publicToken)).
        Execute()
    if err != nil {
        return nil, err
    }

    accessToken := resp.GetAccessToken()

    // IMPORTANT: Encrypt before storing
    encryptedToken, err := s.encryption.Encrypt(accessToken)
    if err != nil {
        return nil, fmt.Errorf("failed to encrypt access token: %w", err)
    }

    // Store encrypted token
    item := &domain.LinkedItem{
        UserID:               userID,
        ItemID:               resp.GetItemId(),
        EncryptedAccessToken: encryptedToken,
        Provider:             "plaid",
    }

    return item, s.repo.Create(ctx, item)
}
```

### QA Environment Limitations

> **Note:** Plaid requires sandbox credentials that are not available in QA environment.
>
> For QA testing:
> - Use unit tests with mocked Plaid responses
> - Use the QA data populator for test data
> - Full Plaid testing requires staging environment

## Paddle Payments

### Environment Configuration

```bash
# .env
PADDLE_VENDOR_ID=your_vendor_id
PADDLE_API_KEY=your_api_key
PADDLE_ENVIRONMENT=sandbox  # sandbox, production
```

### Subscription Tiers

| Tier | Price | Limits |
|------|-------|--------|
| Free | $0 | 1 rule, 3 accounts, 1x/day sync |
| Premium Lite | $4.99/mo | 5 rules, 15 accounts, 2x/day sync |
| Premium | $9.99/mo | 15 rules, 30 accounts, 6x/day sync |
| Premium+ | $19.99/mo | Unlimited, 10min sync, AI + API |

### Webhook Events

```go
// internal/handlers/paddle_webhook_handler.go

func (h *PaddleWebhookHandler) HandleWebhook(ctx context.Context, event paddle.Event) error {
    switch event.EventType {
    case "subscription.activated":
        return h.handleSubscriptionActivated(ctx, event)
    case "subscription.updated":
        return h.handleSubscriptionUpdated(ctx, event)
    case "subscription.canceled":
        return h.handleSubscriptionCanceled(ctx, event)
    case "subscription.past_due":
        return h.handleSubscriptionPastDue(ctx, event)
    default:
        log.Printf("Unhandled Paddle event: %s", event.EventType)
        return nil
    }
}
```

### Subscription Service

```go
// internal/services/subscription_service.go

func (s *subscriptionService) CheckLimit(ctx context.Context, user *domain.User, resource domain.Resource, current int) (*domain.LimitCheckResult, error) {
    tier := s.getTierConfig(user.SubscriptionTier)

    var limit int
    switch resource {
    case domain.ResourceRules:
        limit = tier.MaxRules
    case domain.ResourceAccounts:
        limit = tier.MaxAccounts
    default:
        return nil, fmt.Errorf("unknown resource: %s", resource)
    }

    // Check for user-specific overrides
    override, err := s.overrideRepo.GetByUserAndResource(ctx, user.ID, resource)
    if err == nil && override != nil {
        limit = override.Limit
    }

    return &domain.LimitCheckResult{
        Allowed:     current < limit,
        Current:     current,
        Limit:       limit,
        HasOverride: override != nil,
    }, nil
}
```

### QA Environment Limitations

> **Note:** Paddle webhook testing requires staging environment.
>
> For local testing:
> - Use Paddle's sandbox mode
> - Mock webhook events for unit tests
> - Full payment flow testing requires staging

## Health Checks

```go
// internal/handlers/health_handler.go

type HealthResponse struct {
    Status   string                    `json:"status"`
    Services map[string]ServiceHealth  `json:"services"`
}

type ServiceHealth struct {
    Status  string `json:"status"`
    Message string `json:"message,omitempty"`
}

func (h *HealthHandler) GetHealth(ctx context.Context) (*HealthResponse, error) {
    response := &HealthResponse{
        Status:   "healthy",
        Services: make(map[string]ServiceHealth),
    }

    // Check database
    if err := h.db.PingContext(ctx); err != nil {
        response.Services["database"] = ServiceHealth{Status: "unhealthy", Message: err.Error()}
        response.Status = "degraded"
    } else {
        response.Services["database"] = ServiceHealth{Status: "healthy"}
    }

    // Check Clerk (if configured)
    if h.clerkClient != nil {
        response.Services["clerk"] = ServiceHealth{Status: "configured"}
    }

    return response, nil
}
```

## Debugging Tips

### Clerk Issues

```bash
# Decode JWT to inspect claims
echo $JWT | cut -d'.' -f2 | base64 -d | jq

# Check Clerk logs
fly logs | grep -i clerk
```

### Plaid Issues

```bash
# Check webhook events table
psql $DATABASE_URL -c "SELECT * FROM webhook_events ORDER BY created_at DESC LIMIT 10"

# Check linked items
psql $DATABASE_URL -c "SELECT * FROM linked_items WHERE user_id = 'USER_ID'"
```

### Paddle Issues

```bash
# Check subscription status
curl -H "Authorization: Bearer $TOKEN" http://localhost:8080/api/v1/subscription/status

# Check user tier
psql $DATABASE_URL -c "SELECT subscription_tier FROM users WHERE id = 'USER_ID'"
```
