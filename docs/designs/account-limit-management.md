# Account Limit Management Design Specification

**Issue**: #237
**Created**: 2025-01-25
**Status**: Proposed
**Complexity**: Large (6-8 weeks)

## Executive Summary

BoostedBudget currently has **no enforcement of account limits** despite subscription tiers defining maximum account counts (Free: 3, Premium Lite: 15, Premium: 30). This creates a critical mismatch with Plaid's pricing model where one bank connection (Item) can return 5-10+ accounts.

**The Problem**: When a user connects their bank via Plaid, ALL accounts from that bank are imported and saved without checking tier limits. A Free tier user could accidentally import 10 accounts from a single bank connection, exceeding their 3-account limit.

**The Solution**: Implement smart account prioritization with a two-phase approach:
- **Phase 1 (MVP)**: Auto-select accounts by priority, show locked accounts, enable basic swapping
- **Phase 2 (Advanced)**: User selection UI during linking flow

**Financial Impact**:
- Cost savings: **$16,800/year** (300 free users × 7 excess accounts × $8/account)
- Revenue increase: **$1,800/year** (10% conversion × 300 users × $60/year)
- **Total benefit: $18,600/year**

---

## Table of Contents

1. [Background & Context](#background--context)
2. [Current State Analysis](#current-state-analysis)
3. [Design Options Evaluated](#design-options-evaluated)
4. [Recommended Solution](#recommended-solution)
5. [Technical Implementation](#technical-implementation)
6. [Database Schema Changes](#database-schema-changes)
7. [Backend Implementation](#backend-implementation)
8. [Frontend Implementation](#frontend-implementation)
9. [User Experience Flows](#user-experience-flows)
10. [Testing Strategy](#testing-strategy)
11. [Migration Strategy](#migration-strategy)
12. [Cost Analysis](#cost-analysis)
13. [Metrics & Analytics](#metrics--analytics)
14. [Timeline & Phases](#timeline--phases)
15. [Open Questions](#open-questions)
16. [Acceptance Criteria](#acceptance-criteria)

---

## Background & Context

### Plaid's Pricing Model (Item-Based)

Plaid charges per **Item** (bank connection), NOT per account:

- **Item** = One set of login credentials for a bank
- **Accounts** = Multiple accounts under one Item (checking, savings, credit card, etc.)

**Examples**:
- 3 accounts at Wells Fargo = **1 Item** = $0.60-3.00 one-time fee
- 3 accounts at 3 different banks = **3 Items** = $1.80-9.00 one-time fee

**Cost Structure**:
- Link Token Creation: FREE
- Item Creation (first connection): $0.60 - $3.00 (one-time)
- Auth Product: $0.10 - $0.60 per Item
- Transactions Product: $0.60 - $3.00 per Item
- Account data: FREE (included in Item)

**Key Insight**: Adding 10 accounts from the same bank costs the SAME as adding 1 account from that bank.

### BoostedBudget's Tier Limits (Account-Based)

From `config/subscription_tiers.yaml`:

```yaml
tiers:
  free:
    max_accounts: 3
    max_linked_items: -1  # Not enforced!

  premium_lite:
    max_accounts: 15
    max_linked_items: -1

  premium:
    max_accounts: 30
    max_linked_items: -1

  premium_plus:
    max_accounts: -1  # Unlimited
    max_linked_items: -1
```

**The Mismatch**: Tier limits are account-based, but Plaid returns ALL accounts per Item.

### Multi-Provider Context

While this design focuses on Plaid, the architecture must support future providers:

**Plaid**: Per-Item pricing (one-time fee)
**Yodlee**: Per-Activity pricing (per user per 30 days, unlimited accounts)
**MX**: Per-Item pricing similar to Plaid
**Finicity**: Per-Item pricing similar to Plaid

**Design Requirement**: Solution must work for both Item-based and Activity-based providers.

---

## Current State Analysis

### Critical Bug in `account_service.go`

**Location**: `backend/internal/services/account_service.go` (lines 139-176)

```go
func (s *AccountService) ExchangePublicToken(ctx context.Context, userID string, req *domain.ExchangePublicTokenRequest) (*domain.ExchangePublicTokenResponse, error) {

    // ... authentication, validation ...

    // Step 4: Fetch accounts from Plaid
    accountsResp, _, err := client.PlaidApi.AccountsGet(ctx).AccountsGetRequest(*accountsReq).Execute()
    if err != nil {
        return nil, fmt.Errorf("failed to fetch accounts: %w", err)
    }

    // Step 5: Save accounts to database
    for _, plaidAccount := range accountsResp.GetAccounts() {
        account := convertPlaidAccount(plaidAccount, linkedItem)

        // ❌ NO LIMIT CHECK HERE!
        // All accounts are saved regardless of tier limits
        accountID, err := s.accountRepo.Create(ctx, account)
        if err != nil {
            return nil, fmt.Errorf("failed to save account: %w", err)
        }
        accounts = append(accounts, account)
    }

    return &domain.ExchangePublicTokenResponse{
        LinkedItemID: linkedItem.ID,
        Accounts:     accounts,
    }, nil
}
```

**The Bug**: ALL accounts returned by Plaid are saved to the database without checking:
1. User's subscription tier
2. Current account count
3. Maximum account limit

**Impact**:
- Free tier users (max 3 accounts) can import 10+ accounts
- Costs Plaid API fees for accounts we never intended to support
- Database bloat with inactive accounts
- Potential revenue loss (no upgrade pressure)

### What Happens When User Connects Bank

**Current Flow**:
1. User clicks "Connect Bank Account"
2. Frontend fetches link token from backend
3. User authenticates with Plaid Link
4. Plaid returns public_token + metadata
5. Frontend calls `/plaid/exchange-token` with public_token
6. Backend exchanges for access_token
7. Backend calls Plaid `/accounts/get` → returns ALL accounts (could be 1-20+)
8. **Backend saves ALL accounts without checking limits**
9. User sees all accounts in dashboard

**Real-World Example** (Free Tier User):
- User connects Chase bank
- Chase returns: 2 checking, 2 savings, 1 credit card, 1 investment = **7 accounts**
- BoostedBudget saves all 7 accounts (should only allow 3)
- User gets 4 extra accounts "for free"
- No upgrade pressure
- We pay Plaid fees for unused accounts

---

## Design Options Evaluated

### Option 1: Strict Rejection (Hard Limit)

**Description**: Reject entire bank connection if it would exceed limit.

**Flow**:
```
User connects bank → Plaid returns 7 accounts
Current count: 1 account
Free tier limit: 3 accounts
Total if imported: 1 + 7 = 8 accounts (exceeds limit)
→ Reject connection, show error: "This bank has too many accounts"
```

**Pros**:
- Simple to implement
- Clear boundary enforcement
- No ambiguity about which accounts to keep

**Cons**:
- ❌ Poor user experience (frustrating, confusing)
- ❌ User doesn't know which accounts they're losing access to
- ❌ Blocks legitimate use cases (user only wants 1 checking account)
- ❌ Wastes Plaid's link token creation (already spent API credit)

**Verdict**: ❌ Rejected - UX too poor

---

### Option 2: Smart Auto-Selection (Recommended for MVP)

**Description**: Auto-select accounts by priority, show locked accounts, allow swapping.

**Priority Algorithm**:
```
Depository (checking, savings):    100 points
Credit (credit card, line of credit): 50 points
Loan (mortgage, student, auto):      25 points
Investment (brokerage, retirement):  10 points
Other:                                5 points
```

**Flow**:
```
User connects bank → Plaid returns 7 accounts:
  [1] Checking (depository):  100 points
  [2] Savings (depository):   100 points
  [3] Credit Card (credit):    50 points
  [4] Mortgage (loan):         25 points
  [5] Investment (investment): 10 points
  [6] Savings 2 (depository): 100 points
  [7] Checking 2 (depository): 100 points

Current count: 1 account
Free tier limit: 3 accounts
Available slots: 2

Auto-selected (top 2 by priority):
  ✅ [1] Checking - 100 points
  ✅ [2] Savings - 100 points

Locked (show in UI with upgrade prompt):
  🔒 [3] Credit Card
  🔒 [4] Mortgage
  🔒 [5] Investment
  🔒 [6] Savings 2
  🔒 [7] Checking 2
```

**User Can**:
- View locked accounts
- Swap: Deactivate one active account to activate one locked account
- Upgrade to unlock all accounts

**Pros**:
- ✅ Zero-friction for common case (primary checking/savings auto-selected)
- ✅ Shows value of upgrade (user sees their locked accounts)
- ✅ Simple initial implementation
- ✅ User retains control via swapping

**Cons**:
- ⚠️ Algorithm might not match user's preferences (e.g., they want credit card tracking)
- ⚠️ Requires swap UI for customization

**Verdict**: ✅ Recommended for Phase 1 (MVP)

---

### Option 3: Pre-Flight Account Selection

**Description**: Show account list BEFORE completing connection, let user choose.

**Flow**:
```
User authenticates with bank
  ↓
Plaid returns account metadata
  ↓
Show account selection modal:
  ☐ Checking Account (*****1234)
  ☐ Savings Account (*****5678)
  ☐ Credit Card (*****9012)
  ...
  "Select up to 2 accounts (Free tier: 3 total)"
  [Cancel] [Connect Selected]
  ↓
Only save selected accounts
```

**Pros**:
- ✅ User explicitly chooses which accounts to track
- ✅ No "locked account" confusion
- ✅ Clear upgrade value proposition

**Cons**:
- ❌ Requires Plaid Link customization (complex)
- ❌ Might not be possible with Plaid's Link flow
- ❌ Adds friction to primary use case (most users want all accounts)
- ❌ More UI complexity

**Verdict**: ⚠️ Consider for Phase 2 (Advanced) - requires Plaid Link research

---

### Option 4: Soft Limits with Degraded Service

**Description**: Allow exceeding limits but disable features for excess accounts.

**Flow**:
```
User connects bank → 7 accounts imported
Current count: 1
Limit: 3
Total: 8 accounts

Active (full features): 3 accounts
Read-only (balances only): 5 accounts

Features disabled for read-only accounts:
  - Transaction sync
  - Budget categorization
  - Rule automation
  - Notifications
```

**Pros**:
- ✅ User sees all their accounts
- ✅ Flexibility for users who just want balance visibility

**Cons**:
- ❌ Complex to implement (feature flags per account)
- ❌ Confusing tier distinction
- ❌ Still costs Plaid transaction fees for read-only accounts
- ❌ Undermines upgrade value proposition

**Verdict**: ❌ Rejected - Too complex, unclear value

---

### Option 5: Hybrid (Auto-Select + User Choice)

**Description**: Combine Option 2 (auto-select) with Option 3 (user choice).

**Phase 1 (MVP)**: Auto-select by priority (Option 2)
**Phase 2 (Advanced)**: Add pre-selection UI during linking (Option 3)

**Flow**:
```
PHASE 1 (Initial Implementation):
  → Auto-select by priority
  → Show locked accounts
  → Allow swapping after connection

PHASE 2 (Future Enhancement):
  → Add "Customize" button during Plaid Link flow
  → Show account selection modal
  → User can override auto-selection
  → Fall back to auto-select if user skips
```

**Pros**:
- ✅ Best of both worlds
- ✅ Fast initial implementation (Phase 1 = Option 2)
- ✅ Future flexibility (Phase 2 = Option 3)
- ✅ Progressive enhancement

**Cons**:
- ⚠️ Two-phase implementation (more work)
- ⚠️ Phase 2 depends on Plaid Link customization feasibility

**Verdict**: ✅✅ RECOMMENDED - Implement Phase 1 now, Phase 2 later

---

## Recommended Solution

**Adopt Option 5 (Hybrid Approach)** with two phases:

### Phase 1: Smart Auto-Selection (MVP) - 4 weeks

Implement Option 2 with these components:

1. **Account Prioritization Service** - Score and rank accounts
2. **Account Activation Service** - Activate top N accounts, mark rest as locked
3. **Modified Exchange Token Flow** - Apply limits during account import
4. **Account Swapping API** - Allow users to swap active ↔ locked accounts
5. **Frontend Components**:
   - `AccountLinkSuccessModal` - Show activation results
   - `LockedAccountList` - Display locked accounts with upgrade CTA
   - `AccountSwapInterface` - UI for swapping accounts

### Phase 2: User Selection UI (Advanced) - 2 weeks

Enhance with Option 3 capabilities:

1. **Research Plaid Link Customization** - Investigate technical feasibility
2. **Account Selection Modal** - During linking flow, before saving
3. **Backend Support** - Accept user-selected account IDs
4. **Fallback to Auto-Select** - If user skips selection

**Timeline**: Phase 1 → 4 weeks | Phase 2 → 2 weeks (after Plaid research)

---

## Technical Implementation

### Phase 1: MVP Implementation (4 weeks)

#### Database Schema Changes

**`accounts` table** - Add activation tracking:

```sql
-- Migration: 000006_add_account_activation.up.sql
ALTER TABLE accounts
ADD COLUMN is_active BOOLEAN DEFAULT true NOT NULL,
ADD COLUMN sync_enabled BOOLEAN DEFAULT true NOT NULL,
ADD COLUMN priority_score INTEGER DEFAULT 0 NOT NULL,
ADD COLUMN activation_rank INTEGER,
ADD COLUMN activation_date TIMESTAMP,
ADD COLUMN deactivation_date TIMESTAMP,
ADD COLUMN deactivation_reason TEXT;

-- Index for efficient queries
CREATE INDEX idx_accounts_active ON accounts(user_id, is_active);
CREATE INDEX idx_accounts_priority ON accounts(linked_item_id, priority_score DESC);

-- Update existing accounts to active
UPDATE accounts SET is_active = true, sync_enabled = true WHERE is_active IS NULL;
```

**`subscription_tiers.yaml`** - Add Item limit (future):

```yaml
tiers:
  free:
    max_accounts: 3
    max_linked_items: 5  # Future: limit bank connections
  premium_lite:
    max_accounts: 15
    max_linked_items: 10
  # ... etc
```

**`subscription_overrides` table** - Add custom limits:

```sql
ALTER TABLE subscription_overrides
ADD COLUMN custom_max_accounts INTEGER,
ADD COLUMN custom_max_items INTEGER;
```

---

## Backend Implementation

### New Service: `account_priority_service.go`

**Location**: `backend/internal/services/account_priority_service.go`

```go
package services

import (
    "github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// AccountPriorityService handles account prioritization logic
type AccountPriorityService struct {
    typeScores map[string]int
}

func NewAccountPriorityService() *AccountPriorityService {
    return &AccountPriorityService{
        typeScores: map[string]int{
            "depository": 100, // Checking, savings
            "credit":     50,  // Credit cards
            "loan":       25,  // Mortgages, student loans
            "investment": 10,  // Brokerage, retirement
            "other":      5,   // Unknown types
        },
    }
}

// CalculatePriority calculates priority score for an account
func (s *AccountPriorityService) CalculatePriority(account *domain.Account) int {
    baseScore := s.typeScores[account.Type]

    // Future enhancements:
    // - Boost score for higher balances
    // - Boost score for recent activity
    // - Boost score for primary accounts (detected via account name patterns)

    return baseScore
}

// PrioritizeAccounts sorts accounts by priority score (highest first)
func (s *AccountPriorityService) PrioritizeAccounts(accounts []*domain.Account) []*domain.Account {
    // Calculate priority scores
    for _, account := range accounts {
        account.PriorityScore = s.CalculatePriority(account)
    }

    // Sort by priority score (descending)
    sorted := make([]*domain.Account, len(accounts))
    copy(sorted, accounts)

    sort.Slice(sorted, func(i, j int) bool {
        return sorted[i].PriorityScore > sorted[j].PriorityScore
    })

    return sorted
}
```

---

### New Service: `account_activation_service.go`

**Location**: `backend/internal/services/account_activation_service.go`

```go
package services

import (
    "context"
    "fmt"
    "time"

    "github.com/maxnussbaum/BoostedBudget/internal/domain"
    "github.com/maxnussbaum/BoostedBudget/internal/repository"
)

type AccountActivationService struct {
    accountRepo         repository.AccountRepository
    priorityService     *AccountPriorityService
    subscriptionService SubscriptionService
}

func NewAccountActivationService(
    accountRepo repository.AccountRepository,
    priorityService *AccountPriorityService,
    subscriptionService SubscriptionService,
) *AccountActivationService {
    return &AccountActivationService{
        accountRepo:         accountRepo,
        priorityService:     priorityService,
        subscriptionService: subscriptionService,
    }
}

// ActivateAccountsUpToLimit activates accounts up to user's tier limit
func (s *AccountActivationService) ActivateAccountsUpToLimit(
    ctx context.Context,
    user *domain.User,
    accounts []*domain.Account,
) (*domain.AccountActivationResult, error) {

    // Get user's tier limits
    tier, err := s.subscriptionService.GetUserTier(ctx, user)
    if err != nil {
        return nil, fmt.Errorf("failed to get user tier: %w", err)
    }

    maxAccounts := tier.MaxAccounts
    if maxAccounts == -1 {
        // Unlimited (Premium+)
        maxAccounts = len(accounts)
    }

    // Get current active account count
    currentActive, err := s.accountRepo.CountActiveAccounts(ctx, user.ID)
    if err != nil {
        return nil, fmt.Errorf("failed to count active accounts: %w", err)
    }

    availableSlots := maxAccounts - currentActive
    if availableSlots < 0 {
        availableSlots = 0
    }

    // Prioritize accounts
    prioritized := s.priorityService.PrioritizeAccounts(accounts)

    // Activate top N accounts
    activeAccounts := []*domain.Account{}
    lockedAccounts := []*domain.Account{}

    for i, account := range prioritized {
        if i < availableSlots {
            // Activate account
            account.IsActive = true
            account.SyncEnabled = true
            account.ActivationRank = i + 1
            account.ActivationDate = time.Now()
            activeAccounts = append(activeAccounts, account)
        } else {
            // Lock account
            account.IsActive = false
            account.SyncEnabled = false
            account.DeactivationReason = fmt.Sprintf("Exceeded tier limit (%d accounts)", maxAccounts)
            lockedAccounts = append(lockedAccounts, account)
        }
    }

    return &domain.AccountActivationResult{
        TotalDiscovered:  len(accounts),
        ActiveAccounts:   activeAccounts,
        LockedAccounts:   lockedAccounts,
        AvailableSlots:   availableSlots,
        TierLimit:        maxAccounts,
        TierName:         tier.Name,
    }, nil
}

// SwapAccounts deactivates one account and activates another
func (s *AccountActivationService) SwapAccounts(
    ctx context.Context,
    user *domain.User,
    deactivateID, activateID string,
) error {

    // Validate both accounts belong to user
    deactivateAccount, err := s.accountRepo.GetByID(ctx, deactivateID)
    if err != nil || deactivateAccount.UserID != user.ID {
        return fmt.Errorf("invalid account to deactivate")
    }

    activateAccount, err := s.accountRepo.GetByID(ctx, activateID)
    if err != nil || activateAccount.UserID != user.ID {
        return fmt.Errorf("invalid account to activate")
    }

    // Validate current states
    if !deactivateAccount.IsActive {
        return fmt.Errorf("account %s is already inactive", deactivateID)
    }
    if activateAccount.IsActive {
        return fmt.Errorf("account %s is already active", activateID)
    }

    // Perform swap (transaction)
    err = s.accountRepo.SwapActiveStatus(ctx, deactivateID, activateID)
    if err != nil {
        return fmt.Errorf("failed to swap accounts: %w", err)
    }

    return nil
}
```

---

### Modified: `account_service.go`

**Location**: `backend/internal/services/account_service.go`

**Changes to `ExchangePublicToken` method**:

```go
func (s *AccountService) ExchangePublicToken(
    ctx context.Context,
    userID string,
    req *domain.ExchangePublicTokenRequest,
) (*domain.ExchangePublicTokenResponse, error) {

    // ... existing validation, token exchange ...

    // Step 4: Fetch accounts from Plaid
    accountsResp, _, err := client.PlaidApi.AccountsGet(ctx).AccountsGetRequest(*accountsReq).Execute()
    if err != nil {
        return nil, fmt.Errorf("failed to fetch accounts: %w", err)
    }

    // Convert Plaid accounts to domain accounts
    accounts := []*domain.Account{}
    for _, plaidAccount := range accountsResp.GetAccounts() {
        account := convertPlaidAccount(plaidAccount, linkedItem)
        accounts = append(accounts, account)
    }

    // ✅ NEW: Apply tier-based account activation
    user, err := s.profileRepo.GetByID(ctx, userID)
    if err != nil {
        return nil, fmt.Errorf("failed to get user: %w", err)
    }

    activationResult, err := s.activationService.ActivateAccountsUpToLimit(ctx, user, accounts)
    if err != nil {
        return nil, fmt.Errorf("failed to activate accounts: %w", err)
    }

    // Save all accounts (active + locked) to database
    savedAccounts := []*domain.Account{}
    for _, account := range accounts {
        accountID, err := s.accountRepo.Create(ctx, account)
        if err != nil {
            return nil, fmt.Errorf("failed to save account: %w", err)
        }
        account.ID = accountID
        savedAccounts = append(savedAccounts, account)
    }

    // Return activation result for frontend
    return &domain.ExchangePublicTokenResponse{
        LinkedItemID:     linkedItem.ID,
        Accounts:         savedAccounts,
        ActivationResult: activationResult, // ✅ NEW
    }, nil
}
```

---

### New API Endpoint: Account Swapping

**Location**: `backend/internal/handlers/plaid_handler.go`

```go
// SwapAccounts allows user to swap active ↔ locked accounts
func (h *PlaidHandler) SwapAccounts(c echo.Context) error {
    ctx := c.Request().Context()
    userID := auth.GetUserID(ctx)

    var req struct {
        DeactivateAccountID string `json:"deactivate_account_id" validate:"required"`
        ActivateAccountID   string `json:"activate_account_id" validate:"required"`
    }

    if err := c.Bind(&req); err != nil {
        return echo.NewHTTPError(http.StatusBadRequest, "Invalid request")
    }

    if err := c.Validate(&req); err != nil {
        return echo.NewHTTPError(http.StatusBadRequest, err.Error())
    }

    user, err := h.profileService.GetByID(ctx, userID)
    if err != nil {
        return echo.NewHTTPError(http.StatusInternalServerError, "Failed to get user")
    }

    err = h.accountService.SwapAccounts(ctx, user, req.DeactivateAccountID, req.ActivateAccountID)
    if err != nil {
        return echo.NewHTTPError(http.StatusBadRequest, err.Error())
    }

    return c.JSON(http.StatusOK, map[string]string{
        "message": "Accounts swapped successfully",
    })
}
```

**Route Registration** (`backend/cmd/server/main.go`):

```go
api.POST("/accounts/swap", plaidHandler.SwapAccounts)
```

---

## Frontend Implementation

### Updated API Client: `plaidApi.ts`

**Location**: `frontend/src/features/accounts/api/plaidApi.ts`

```typescript
export interface AccountActivationResult {
  total_discovered: number
  active_accounts: Account[]
  locked_accounts: Account[]
  available_slots: number
  tier_limit: number
  tier_name: string
}

export interface ExchangePublicTokenResponse {
  linked_item_id: string
  accounts: Account[]
  activation_result: AccountActivationResult // ✅ NEW
}

export async function swapAccounts(
  deactivateAccountId: string,
  activateAccountId: string
): Promise<void> {
  const response = await fetch(`${API_URL}/accounts/swap`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      Authorization: `Bearer ${getAccessToken()}`,
    },
    body: JSON.stringify({
      deactivate_account_id: deactivateAccountId,
      activate_account_id: activateAccountId,
    }),
  })

  if (!response.ok) {
    throw new Error(await response.text())
  }
}
```

---

### New Component: `AccountLinkSuccessModal.tsx`

**Location**: `frontend/src/features/accounts/components/AccountLinkSuccessModal.tsx`

```typescript
import { useState } from 'react'
import type { AccountActivationResult } from '../api/plaidApi'
import './AccountLinkSuccessModal.css'

interface AccountLinkSuccessModalProps {
  result: AccountActivationResult
  onClose: () => void
}

export function AccountLinkSuccessModal({ result, onClose }: AccountLinkSuccessModalProps) {
  const hasLockedAccounts = result.locked_accounts.length > 0

  return (
    <div className="modal-overlay">
      <div className="modal-content">
        <h2>✅ Bank Connected Successfully</h2>

        {/* Active Accounts */}
        <div className="section">
          <h3>Active Accounts ({result.active_accounts.length})</h3>
          <ul className="account-list">
            {result.active_accounts.map((account) => (
              <li key={account.id} className="account-item active">
                <span className="account-name">{account.name}</span>
                <span className="account-type">{account.type}</span>
                <span className="account-balance">${account.current_balance?.toFixed(2)}</span>
              </li>
            ))}
          </ul>
        </div>

        {/* Locked Accounts */}
        {hasLockedAccounts && (
          <div className="section locked-section">
            <h3>🔒 Locked Accounts ({result.locked_accounts.length})</h3>
            <p className="tier-limit-message">
              Your <strong>{result.tier_name}</strong> tier includes up to {result.tier_limit} accounts.
              Upgrade to unlock all {result.total_discovered} accounts.
            </p>

            <ul className="account-list locked">
              {result.locked_accounts.map((account) => (
                <li key={account.id} className="account-item locked">
                  <span className="account-name">{account.name}</span>
                  <span className="account-type">{account.type}</span>
                  <span className="lock-icon">🔒</span>
                </li>
              ))}
            </ul>

            <div className="upgrade-cta">
              <button className="btn-upgrade">
                Upgrade to Premium - Unlock All Accounts
              </button>
              <button className="btn-swap">
                Or Swap Active Accounts
              </button>
            </div>
          </div>
        )}

        <button className="btn-close" onClick={onClose}>
          Done
        </button>
      </div>
    </div>
  )
}
```

---

### New Component: `AccountSwapInterface.tsx`

**Location**: `frontend/src/features/accounts/components/AccountSwapInterface.tsx`

```typescript
import { useState } from 'react'
import { swapAccounts } from '../api/plaidApi'
import type { Account } from '../types'
import './AccountSwapInterface.css'

interface AccountSwapInterfaceProps {
  activeAccounts: Account[]
  lockedAccounts: Account[]
  onSwapComplete: () => void
}

export function AccountSwapInterface({
  activeAccounts,
  lockedAccounts,
  onSwapComplete,
}: AccountSwapInterfaceProps) {
  const [selectedActive, setSelectedActive] = useState<string | null>(null)
  const [selectedLocked, setSelectedLocked] = useState<string | null>(null)
  const [swapping, setSwapping] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const canSwap = selectedActive !== null && selectedLocked !== null

  const handleSwap = async () => {
    if (!canSwap) return

    try {
      setSwapping(true)
      setError(null)
      await swapAccounts(selectedActive, selectedLocked)
      onSwapComplete()
    } catch (err) {
      setError('Failed to swap accounts. Please try again.')
      console.error('Swap error:', err)
    } finally {
      setSwapping(false)
    }
  }

  return (
    <div className="account-swap-interface">
      <h3>Swap Accounts</h3>
      <p className="swap-instructions">
        Select one active account to deactivate and one locked account to activate.
      </p>

      <div className="swap-columns">
        {/* Active Accounts Column */}
        <div className="swap-column">
          <h4>Active Accounts (Deactivate)</h4>
          <ul className="swap-account-list">
            {activeAccounts.map((account) => (
              <li
                key={account.id}
                className={`swap-account-item ${
                  selectedActive === account.id ? 'selected' : ''
                }`}
                onClick={() => setSelectedActive(account.id)}
              >
                <input
                  type="radio"
                  name="active-account"
                  checked={selectedActive === account.id}
                  onChange={() => setSelectedActive(account.id)}
                />
                <span className="account-name">{account.name}</span>
                <span className="account-type">{account.type}</span>
              </li>
            ))}
          </ul>
        </div>

        {/* Swap Arrow */}
        <div className="swap-arrow">
          <span>⇄</span>
        </div>

        {/* Locked Accounts Column */}
        <div className="swap-column">
          <h4>Locked Accounts (Activate)</h4>
          <ul className="swap-account-list">
            {lockedAccounts.map((account) => (
              <li
                key={account.id}
                className={`swap-account-item ${
                  selectedLocked === account.id ? 'selected' : ''
                }`}
                onClick={() => setSelectedLocked(account.id)}
              >
                <input
                  type="radio"
                  name="locked-account"
                  checked={selectedLocked === account.id}
                  onChange={() => setSelectedLocked(account.id)}
                />
                <span className="account-name">{account.name}</span>
                <span className="account-type">{account.type}</span>
                <span className="lock-icon">🔒</span>
              </li>
            ))}
          </ul>
        </div>
      </div>

      {error && <p className="swap-error">{error}</p>}

      <button
        className="btn-swap-confirm"
        disabled={!canSwap || swapping}
        onClick={handleSwap}
      >
        {swapping ? 'Swapping...' : 'Swap Accounts'}
      </button>
    </div>
  )
}
```

---

### Modified: `PlaidLinkButton.tsx`

**Location**: `frontend/src/features/accounts/PlaidLinkButton.tsx`

**Changes to `handleOnSuccess`**:

```typescript
const handleOnSuccess = async (public_token: string, metadata: PlaidLinkOnSuccessMetadata) => {
  try {
    setExchanging(true)
    setError(null)

    const response = await exchangePublicToken({
      public_token,
      institution_id: metadata.institution?.institution_id || '',
      institution_name: metadata.institution?.name || '',
    })

    // ✅ NEW: Show activation result modal
    if (response.activation_result) {
      setActivationResult(response.activation_result)
      setShowResultModal(true)
    }

    // Notify parent component if callback provided
    onSuccess?.(response)

    // Navigate to accounts page after modal close
  } catch (err) {
    console.error('Failed to exchange public token:', err)
    setError(mapErrorToMessage(err))
  } finally {
    setExchanging(false)
  }
}

// ✅ NEW: Render activation result modal
{showResultModal && activationResult && (
  <AccountLinkSuccessModal
    result={activationResult}
    onClose={() => {
      setShowResultModal(false)
      navigate('/accounts')
    }}
  />
)}
```

---

## User Experience Flows

### Flow 1: Free Tier User - Within Limit

**Scenario**: User with 1 existing account connects bank with 2 accounts (total: 3, limit: 3)

**Steps**:
1. User clicks "Connect Bank Account"
2. Plaid Link flow completes
3. Backend receives 2 accounts from Plaid
4. Priority service scores accounts:
   - Checking: 100 points
   - Savings: 100 points
5. Activation service checks limit:
   - Current: 1 account
   - Limit: 3 accounts
   - Available slots: 2
   - Result: Activate both accounts
6. Frontend shows success modal:
   ```
   ✅ Bank Connected Successfully

   Active Accounts (2):
   - Checking Account (****1234) - $2,500.00
   - Savings Account (****5678) - $10,000.00

   [Done]
   ```
7. User clicks "Done" → navigates to accounts page
8. Dashboard shows all 3 accounts (1 existing + 2 new)

**Outcome**: ✅ Smooth experience, no friction

---

### Flow 2: Free Tier User - Exceeds Limit (Common Case)

**Scenario**: User with 0 accounts connects Chase bank with 7 accounts (limit: 3)

**Steps**:
1. User clicks "Connect Bank Account"
2. Plaid Link flow completes
3. Backend receives 7 accounts from Plaid:
   - Checking (****1234): depository, $2,500
   - Savings (****5678): depository, $10,000
   - Credit Card (****9012): credit, $0 (-$1,200 balance)
   - Savings 2 (****3456): depository, $5,000
   - Investment (****7890): investment, $50,000
   - Mortgage (****2345): loan, $0 (-$250,000 balance)
   - Auto Loan (****6789): loan, $0 (-$15,000 balance)

4. Priority service scores accounts:
   - Checking: 100 points
   - Savings: 100 points
   - Savings 2: 100 points
   - Credit Card: 50 points
   - Mortgage: 25 points
   - Auto Loan: 25 points
   - Investment: 10 points

5. Activation service applies limit:
   - Current: 0 accounts
   - Limit: 3 accounts
   - Available slots: 3
   - **Activate top 3**: Checking, Savings, Savings 2
   - **Lock remaining 4**: Credit Card, Investment, Mortgage, Auto Loan

6. Frontend shows success modal:
   ```
   ✅ Bank Connected Successfully

   Active Accounts (3):
   - Checking Account (****1234) - $2,500.00
   - Savings Account (****5678) - $10,000.00
   - Savings Account 2 (****3456) - $5,000.00

   🔒 Locked Accounts (4):
   Your Free tier includes up to 3 accounts.
   Upgrade to unlock all 7 accounts.

   - Credit Card (****9012) 🔒
   - Investment Account (****7890) 🔒
   - Mortgage (****2345) 🔒
   - Auto Loan (****6789) 🔒

   [Upgrade to Premium - Unlock All Accounts]
   [Or Swap Active Accounts]
   [Done]
   ```

7. User has 3 options:
   - **Option A**: Click "Upgrade" → navigates to pricing page
   - **Option B**: Click "Swap" → opens swap interface
   - **Option C**: Click "Done" → navigates to accounts page (accepts auto-selection)

**Outcome**:
- ✅ User sees their locked accounts (value proposition)
- ✅ Upgrade CTA prominently displayed
- ✅ Flexibility to swap if they disagree with auto-selection

---

### Flow 3: Free Tier User - Wants Different Accounts (Swapping)

**Scenario**: Continuing from Flow 2, user wants to track Credit Card instead of Savings 2

**Steps**:
1. User clicks "Or Swap Active Accounts" in success modal
2. Swap interface opens:
   ```
   Swap Accounts
   Select one active account to deactivate and one locked account to activate.

   Active Accounts (Deactivate)     ⇄     Locked Accounts (Activate)
   ○ Checking (****1234)                  ○ Credit Card (****9012) 🔒
   ○ Savings (****5678)                   ○ Investment (****7890) 🔒
   ⦿ Savings 2 (****3456)                 ⦿ Mortgage (****2345) 🔒
                                          ○ Auto Loan (****6789) 🔒

   [Swap Accounts]
   ```

3. User selects:
   - Deactivate: Savings 2 (****3456)
   - Activate: Credit Card (****9012)

4. User clicks "Swap Accounts"
5. Backend validates:
   - Both accounts belong to user ✓
   - Savings 2 is currently active ✓
   - Credit Card is currently locked ✓
6. Backend performs swap (atomic transaction):
   - Deactivate Savings 2: `is_active = false`, `sync_enabled = false`
   - Activate Credit Card: `is_active = true`, `sync_enabled = true`
7. Frontend shows success:
   ```
   ✅ Accounts Swapped Successfully

   Active Accounts (3):
   - Checking Account (****1234) - $2,500.00
   - Savings Account (****5678) - $10,000.00
   - Credit Card (****9012) - $0.00

   Locked Accounts (4):
   - Savings Account 2 (****3456) 🔒
   - Investment Account (****7890) 🔒
   - Mortgage (****2345) 🔒
   - Auto Loan (****6789) 🔒

   [Done]
   ```

8. User clicks "Done" → navigates to accounts page
9. Dashboard shows:
   - Active: Checking, Savings, Credit Card
   - Locked: Savings 2, Investment, Mortgage, Auto Loan

**Outcome**:
- ✅ User has control over which accounts are tracked
- ✅ Simple swap interface (radio buttons + confirm)
- ✅ Immediate feedback

---

### Flow 4: Premium Tier User - No Limits

**Scenario**: Premium user (30 account limit) connects bank with 7 accounts (well under limit)

**Steps**:
1. User clicks "Connect Bank Account"
2. Plaid Link flow completes
3. Backend receives 7 accounts from Plaid
4. Priority service scores accounts (same as before)
5. Activation service checks limit:
   - Current: 5 accounts
   - Limit: 30 accounts
   - Available slots: 25
   - Result: **Activate all 7 accounts**
6. Frontend shows simplified success modal:
   ```
   ✅ Bank Connected Successfully

   7 accounts added to your dashboard:
   - Checking Account (****1234) - $2,500.00
   - Savings Account (****5678) - $10,000.00
   - Credit Card (****9012) - $0.00
   - Savings Account 2 (****3456) - $5,000.00
   - Investment Account (****7890) - $50,000.00
   - Mortgage (****2345) - $0.00
   - Auto Loan (****6789) - $0.00

   [Done]
   ```
7. User clicks "Done" → navigates to accounts page
8. Dashboard shows all 12 accounts (5 existing + 7 new)

**Outcome**:
- ✅ Zero friction for premium users
- ✅ No locked accounts, no upgrade prompts
- ✅ All accounts immediately available

---

## Testing Strategy

### Unit Tests (Backend)

**`account_priority_service_test.go`**:
```go
func TestCalculatePriority(t *testing.T) {
    service := NewAccountPriorityService()

    tests := []struct {
        accountType     string
        expectedScore   int
    }{
        {"depository", 100},
        {"credit", 50},
        {"loan", 25},
        {"investment", 10},
        {"other", 5},
        {"unknown_type", 5}, // Default to 'other'
    }

    for _, tt := range tests {
        account := &domain.Account{Type: tt.accountType}
        score := service.CalculatePriority(account)
        assert.Equal(t, tt.expectedScore, score)
    }
}

func TestPrioritizeAccounts(t *testing.T) {
    service := NewAccountPriorityService()

    accounts := []*domain.Account{
        {ID: "1", Type: "investment", Name: "Investment"},   // 10 points
        {ID: "2", Type: "depository", Name: "Checking"},     // 100 points
        {ID: "3", Type: "credit", Name: "Credit Card"},      // 50 points
        {ID: "4", Type: "loan", Name: "Mortgage"},           // 25 points
        {ID: "5", Type: "depository", Name: "Savings"},      // 100 points
    }

    prioritized := service.PrioritizeAccounts(accounts)

    // Expected order: Checking (100), Savings (100), Credit Card (50), Mortgage (25), Investment (10)
    assert.Equal(t, "Checking", prioritized[0].Name)
    assert.Equal(t, "Savings", prioritized[1].Name)
    assert.Equal(t, "Credit Card", prioritized[2].Name)
    assert.Equal(t, "Mortgage", prioritized[3].Name)
    assert.Equal(t, "Investment", prioritized[4].Name)
}
```

**`account_activation_service_test.go`**:
```go
func TestActivateAccountsUpToLimit_FreeTier(t *testing.T) {
    // Setup mocks
    accountRepo := &repository.MockAccountRepository{}
    subscriptionService := &services.MockSubscriptionService{}
    priorityService := NewAccountPriorityService()

    service := NewAccountActivationService(accountRepo, priorityService, subscriptionService)

    // Mock user with free tier (3 account limit)
    user := &domain.User{ID: "user123"}
    tier := &domain.Tier{Name: "free", MaxAccounts: 3}

    subscriptionService.On("GetUserTier", mock.Anything, user).Return(tier, nil)
    accountRepo.On("CountActiveAccounts", mock.Anything, user.ID).Return(0, nil)

    // 7 accounts from Plaid
    accounts := []*domain.Account{
        {ID: "1", Type: "depository", Name: "Checking"},
        {ID: "2", Type: "depository", Name: "Savings"},
        {ID: "3", Type: "credit", Name: "Credit Card"},
        {ID: "4", Type: "depository", Name: "Savings 2"},
        {ID: "5", Type: "investment", Name: "Investment"},
        {ID: "6", Type: "loan", Name: "Mortgage"},
        {ID: "7", Type: "loan", Name: "Auto Loan"},
    }

    // Execute
    result, err := service.ActivateAccountsUpToLimit(context.Background(), user, accounts)

    // Assert
    require.NoError(t, err)
    assert.Equal(t, 7, result.TotalDiscovered)
    assert.Equal(t, 3, len(result.ActiveAccounts))
    assert.Equal(t, 4, len(result.LockedAccounts))
    assert.Equal(t, 3, result.AvailableSlots)

    // Verify top 3 by priority
    assert.Contains(t, []string{"Checking", "Savings", "Savings 2"}, result.ActiveAccounts[0].Name)
    assert.True(t, result.ActiveAccounts[0].IsActive)
    assert.False(t, result.LockedAccounts[0].IsActive)
}

func TestSwapAccounts_Success(t *testing.T) {
    accountRepo := &repository.MockAccountRepository{}
    service := NewAccountActivationService(accountRepo, nil, nil)

    user := &domain.User{ID: "user123"}
    activeAccount := &domain.Account{ID: "active1", UserID: "user123", IsActive: true}
    lockedAccount := &domain.Account{ID: "locked1", UserID: "user123", IsActive: false}

    accountRepo.On("GetByID", mock.Anything, "active1").Return(activeAccount, nil)
    accountRepo.On("GetByID", mock.Anything, "locked1").Return(lockedAccount, nil)
    accountRepo.On("SwapActiveStatus", mock.Anything, "active1", "locked1").Return(nil)

    err := service.SwapAccounts(context.Background(), user, "active1", "locked1")

    require.NoError(t, err)
    accountRepo.AssertCalled(t, "SwapActiveStatus", mock.Anything, "active1", "locked1")
}
```

---

### Integration Tests (Backend)

**`account_linking_integration_test.go`**:
```go
//go:build integration

func TestExchangePublicToken_WithLimitEnforcement(t *testing.T) {
    // Setup test database, real services
    db := setupTestDB(t)
    defer db.Close()

    // Create free tier user with 1 existing account
    user := createTestUser(t, db, "free", 1)

    // Mock Plaid client to return 7 accounts
    plaidClient := &mockPlaidClient{
        accounts: createMockPlaidAccounts(7),
    }

    // Real services
    accountService := createAccountService(db, plaidClient)

    // Execute public token exchange
    response, err := accountService.ExchangePublicToken(ctx, user.ID, &domain.ExchangePublicTokenRequest{
        PublicToken:     "test-public-token",
        InstitutionID:   "ins_test",
        InstitutionName: "Test Bank",
    })

    require.NoError(t, err)

    // Verify activation result
    assert.Equal(t, 7, response.ActivationResult.TotalDiscovered)
    assert.Equal(t, 2, len(response.ActivationResult.ActiveAccounts)) // 1 existing + 2 available slots
    assert.Equal(t, 5, len(response.ActivationResult.LockedAccounts))

    // Verify database state
    activeCount := countActiveAccounts(t, db, user.ID)
    assert.Equal(t, 3, activeCount) // Reached tier limit
}
```

---

### E2E Tests (Frontend)

**`account-linking.spec.ts`**:
```typescript
import { test, expect } from '@playwright/test'

test('Free tier user - exceeds limit shows locked accounts', async ({ page }) => {
  // Setup: Login as free tier user
  await page.goto('/login')
  await loginAsFreeTierUser(page)

  // Mock Plaid Link flow to return 7 accounts
  await mockPlaidLink(page, {
    accounts: 7,
    publicToken: 'test-public-token',
  })

  // Click "Connect Bank Account"
  await page.click('button:has-text("Connect Bank Account")')

  // Wait for Plaid Link modal
  await page.waitForSelector('.plaid-link-modal')

  // Complete Plaid Link (mocked)
  await completePlaidLink(page)

  // Wait for success modal
  await page.waitForSelector('.account-link-success-modal')

  // Verify active accounts section
  const activeAccounts = page.locator('.section:has-text("Active Accounts")')
  await expect(activeAccounts).toContainText('Active Accounts (3)')

  // Verify locked accounts section
  const lockedAccounts = page.locator('.section:has-text("Locked Accounts")')
  await expect(lockedAccounts).toContainText('Locked Accounts (4)')
  await expect(lockedAccounts).toContainText('Your Free tier includes up to 3 accounts')

  // Verify upgrade CTA
  await expect(page.locator('button:has-text("Upgrade to Premium")')).toBeVisible()

  // Verify swap button
  await expect(page.locator('button:has-text("Swap Active Accounts")')).toBeVisible()
})

test('Account swapping - deactivate one, activate another', async ({ page }) => {
  // Setup: Free tier user with 3 active + 4 locked accounts
  await setupUserWithAccounts(page, { active: 3, locked: 4 })

  // Navigate to accounts page
  await page.goto('/accounts')

  // Click "Swap Accounts" button
  await page.click('button:has-text("Swap Accounts")')

  // Wait for swap interface
  await page.waitForSelector('.account-swap-interface')

  // Select account to deactivate (Savings 2)
  await page.click('.swap-account-list >> text=Savings 2')

  // Select account to activate (Credit Card)
  await page.click('.swap-account-list >> text=Credit Card')

  // Click "Swap Accounts" confirm button
  await page.click('button:has-text("Swap Accounts")')

  // Wait for success message
  await expect(page.locator('text=Accounts Swapped Successfully')).toBeVisible()

  // Verify new account list
  await expect(page.locator('.active >> text=Credit Card')).toBeVisible()
  await expect(page.locator('.locked >> text=Savings 2')).toBeVisible()
})
```

---

## Migration Strategy

### Existing Users (Grandfathering)

**Goal**: Do NOT disrupt existing users who already have accounts exceeding their tier limits.

**Approach**:
1. **Mark all existing accounts as active**: Run migration to set `is_active = true` for all current accounts
2. **Apply limits only to NEW connections**: Limits enforced ONLY when user connects a NEW bank
3. **Count existing accounts toward limit**: When connecting new bank, include existing active accounts in count

**Example**:
- Free tier user has 5 existing accounts (from before this feature)
- Migration sets all 5 as `is_active = true`
- User connects new bank with 3 accounts
- Activation service counts: 5 existing + 3 new = 8 total
- Limit: 3 accounts
- Result: 0 available slots → **all 3 new accounts are locked**
- Existing 5 accounts remain active (grandfathered)
- User sees: "Your account is full. Upgrade to activate new accounts."

**Migration SQL**:
```sql
-- Set all existing accounts to active
UPDATE accounts SET is_active = true, sync_enabled = true WHERE is_active IS NULL;

-- Add note to user_metadata for grandfathered users
INSERT INTO user_metadata (user_id, key, value)
SELECT DISTINCT user_id, 'grandfathered_account_limit', 'true'
FROM accounts
GROUP BY user_id
HAVING COUNT(*) > (
    SELECT COALESCE(
        (SELECT custom_max_accounts FROM subscription_overrides WHERE subscription_overrides.user_id = accounts.user_id),
        (SELECT max_accounts FROM subscription_tiers WHERE tier_name = (SELECT tier FROM user_profiles WHERE user_profiles.id = accounts.user_id))
    )
);
```

### Communication to Users

**Email Template** (sent to grandfathered users):

```
Subject: Important Update: Account Management Changes

Hi [User Name],

We're introducing smarter account management to BoostedBudget!

**What's Changing:**
Starting [Date], your [Tier Name] tier includes up to [Limit] accounts.

**Your Current Accounts:**
You currently have [Count] accounts connected, which is above the new limit.
Don't worry - all your existing accounts will continue to work normally.

**What This Means:**
✅ Your existing [Count] accounts remain active
⚠️ New bank connections will be limited to your tier's account limit
🚀 Upgrade to unlock unlimited accounts

**Why This Change:**
This helps us provide better service and keeps our costs sustainable.

**Questions?**
Contact support@boostedbudget.com

[Upgrade to Premium] [Learn More]
```

---

## Cost Analysis

### Current State (No Limit Enforcement)

**Assumptions**:
- 1,000 free tier users
- 30% connect banks (300 users)
- Average 7 accounts per bank connection
- Plaid cost: $8/account/year (transaction sync)

**Current Costs**:
```
300 users × 7 accounts × $8/year = $16,800/year
```

### After Implementation (With Limits)

**Free Tier**:
- 300 users × 3 accounts × $8/year = $7,200/year

**Conversions** (estimated 10% upgrade due to locked accounts):
- 30 users upgrade to Premium Lite ($4.99/month = $60/year)
- Revenue: 30 × $60 = $1,800/year
- Premium Lite costs: 30 users × 15 accounts × $8/year = $3,600/year

**Total After Implementation**:
- Free tier costs: $7,200
- Premium Lite costs: $3,600
- Total costs: $10,800/year
- Revenue from conversions: $1,800/year

**Net Savings**:
```
Before: $16,800/year (costs only)
After:  $10,800/year (costs) - $1,800/year (revenue) = $9,000/year net
Savings: $16,800 - $9,000 = $7,800/year
```

**Plus Revenue Upside**:
- Additional $1,800/year from conversions
- **Total benefit: $16,800 - $10,800 + $1,800 = $7,800 + $1,800 = $9,600/year**

*(Note: Conservative estimates. Actual conversion rate may be higher.)*

---

## Metrics & Analytics

### Track These Metrics

**Account Linking**:
- `account_links_total`: Total bank connections
- `accounts_discovered_per_link`: Distribution (1-20+)
- `accounts_activated_per_link`: How many auto-activated
- `accounts_locked_per_link`: How many locked

**Activation Decisions**:
- `activation_by_tier`: Active/locked breakdown per tier
- `activation_by_account_type`: Which types get activated most (depository > credit > loan > investment)
- `auto_selection_acceptance_rate`: % users who accept auto-selection vs swap

**User Behavior**:
- `account_swaps_total`: How many swaps performed
- `account_swap_patterns`: Which account types are swapped most
- `locked_account_views`: How many users view locked accounts
- `upgrade_cta_clicks`: Click-through rate on "Upgrade" button

**Conversions**:
- `locked_accounts_conversion_rate`: % users with locked accounts who upgrade
- `time_to_conversion`: Days from first locked account to upgrade
- `conversion_by_locked_count`: Correlation between # locked accounts and conversion rate

**Technical**:
- `prioritization_duration_ms`: Performance of priority algorithm
- `activation_duration_ms`: Performance of activation service
- `swap_errors`: Failed swap attempts

### Analytics Implementation

**Backend (Prometheus metrics)**:
```go
var (
    accountLinksTotal = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "account_links_total",
            Help: "Total number of bank connections",
        },
        []string{"tier", "institution"},
    )

    accountsDiscoveredPerLink = prometheus.NewHistogramVec(
        prometheus.HistogramOpts{
            Name:    "accounts_discovered_per_link",
            Help:    "Number of accounts discovered per bank connection",
            Buckets: prometheus.LinearBuckets(1, 1, 20),
        },
        []string{"tier"},
    )

    accountsActivatedPerLink = prometheus.NewHistogramVec(
        prometheus.HistogramOpts{
            Name:    "accounts_activated_per_link",
            Help:    "Number of accounts activated per bank connection",
            Buckets: prometheus.LinearBuckets(1, 1, 20),
        },
        []string{"tier"},
    )
)

func init() {
    prometheus.MustRegister(
        accountLinksTotal,
        accountsDiscoveredPerLink,
        accountsActivatedPerLink,
    )
}
```

**Frontend (Analytics events)**:
```typescript
// When activation result shown
analytics.track('Account Link Success', {
  total_discovered: result.total_discovered,
  active_accounts: result.active_accounts.length,
  locked_accounts: result.locked_accounts.length,
  tier: result.tier_name,
})

// When upgrade CTA clicked
analytics.track('Upgrade CTA Clicked', {
  source: 'locked_accounts_modal',
  locked_count: result.locked_accounts.length,
  tier: result.tier_name,
})

// When account swap performed
analytics.track('Account Swapped', {
  deactivated_type: deactivatedAccount.type,
  activated_type: activatedAccount.type,
  tier: user.tier,
})
```

---

## Timeline & Phases

### Phase 1: MVP Implementation (4 weeks)

**Week 1: Backend Foundation**
- Database migration (`000006_add_account_activation.up.sql`)
- Create `account_priority_service.go`
- Create `account_activation_service.go`
- Unit tests for priority and activation services

**Week 2: Backend Integration**
- Modify `account_service.go` to apply limits
- Add `/accounts/swap` API endpoint
- Integration tests for limit enforcement
- Update `domain` models

**Week 3: Frontend Components**
- Create `AccountLinkSuccessModal.tsx`
- Create `AccountSwapInterface.tsx`
- Update `PlaidLinkButton.tsx` to show modal
- Update `plaidApi.ts` with new endpoints

**Week 4: Testing & Polish**
- E2E tests (Playwright)
- User acceptance testing
- Analytics implementation
- Documentation updates
- Migration script for existing users
- Deploy to staging → production

**Deliverables**:
- ✅ Account limit enforcement on new connections
- ✅ Smart auto-selection by priority
- ✅ Locked account visibility with upgrade CTAs
- ✅ Account swapping functionality
- ✅ Grandfathering for existing users

---

### Phase 2: Advanced User Selection (2 weeks)

**Prerequisites**:
- Phase 1 deployed and stable
- Research Plaid Link customization options

**Week 1: Plaid Link Research & Prototyping**
- Investigate Plaid Link `onEvent` callbacks
- Prototype account selection modal integration
- Determine technical feasibility
- Design UX flow for selection

**Week 2: Implementation (if feasible)**
- Backend: Accept user-selected account IDs
- Frontend: Account selection modal during link flow
- Fallback to auto-selection if user skips
- Testing and deployment

**Deliverables**:
- ✅ User can choose accounts during link flow
- ✅ Fallback to Phase 1 auto-selection
- ✅ Improved user experience for power users

---

## Open Questions

1. **Priority Algorithm Enhancement**:
   - Should we boost priority for accounts with higher balances?
   - Should we boost priority for accounts with recent transaction activity?
   - Should we detect "primary" accounts via naming patterns (e.g., "Main Checking")?

2. **Grandfathering Edge Cases**:
   - What if grandfathered user deletes an account? Can they re-activate it later?
   - Should grandfathered users see a one-time "Account Limit Update" modal?

3. **Premium+ Users**:
   - Unlimited accounts - should we still show priority scores for organizational purposes?
   - Should we add "Organize by Priority" feature for users with 50+ accounts?

4. **Multi-Provider Context**:
   - When Yodlee is added (per-Activity pricing), should we track which provider returned each account?
   - Should activation logic differ by provider? (e.g., Yodlee = activate all since it's per-activity)

5. **User Communication**:
   - Should we send email when user hits account limit?
   - Should we show in-app notification: "You have 4 locked accounts - upgrade to unlock"?

6. **Analytics**:
   - What's the minimum conversion rate needed to justify this feature?
   - Should we A/B test auto-selection vs user selection (Phase 1 vs Phase 2)?

7. **Plaid Link Customization**:
   - Is it technically possible to show account selection modal DURING Plaid Link flow?
   - Or must we show it AFTER link completes? (Phase 1 approach)

---

## Acceptance Criteria

### Phase 1 (MVP)

**Functional Requirements**:
- [x] ✅ Account limit enforcement during public token exchange
- [x] ✅ Smart auto-selection by account type priority (depository > credit > loan > investment)
- [x] ✅ Locked accounts saved to database with `is_active = false`
- [x] ✅ Active accounts saved with `is_active = true`, `sync_enabled = true`
- [x] ✅ Account swapping API endpoint (`POST /accounts/swap`)
- [x] ✅ Frontend modal showing activation results (active + locked accounts)
- [x] ✅ Frontend swap interface (deactivate one, activate one)
- [x] ✅ Upgrade CTA prominently displayed when accounts are locked
- [x] ✅ Grandfathering migration: existing accounts marked active
- [x] ✅ Premium+ users (unlimited) activate all accounts

**Non-Functional Requirements**:
- [x] ✅ Unit test coverage: >80% for priority and activation services
- [x] ✅ Integration test: Full exchange-token flow with limits
- [x] ✅ E2E test: Plaid linking with locked accounts modal
- [x] ✅ Performance: Prioritization < 100ms for 20 accounts
- [x] ✅ Performance: Activation service < 200ms
- [x] ✅ Analytics: Track account link events, swaps, upgrade CTAs
- [x] ✅ Documentation: User-facing help article "Managing Your Accounts"

**User Experience**:
- [x] ✅ Free tier user connecting 7 accounts sees 3 active + 4 locked
- [x] ✅ User can swap accounts via simple UI (radio buttons + confirm)
- [x] ✅ Premium tier user connecting 7 accounts sees all 7 active
- [x] ✅ Clear messaging: "Your Free tier includes up to 3 accounts"
- [x] ✅ Upgrade button navigates to pricing page

---

### Phase 2 (Advanced) - Future

**Functional Requirements**:
- [ ] ⏳ Plaid Link customization research completed
- [ ] ⏳ Account selection modal during link flow (if feasible)
- [ ] ⏳ Backend accepts user-selected account IDs
- [ ] ⏳ Fallback to Phase 1 auto-selection if user skips
- [ ] ⏳ A/B test: Phase 1 vs Phase 2 conversion rates

---

## Summary

This design document specifies a two-phase approach to implementing account limit management:

**Phase 1 (MVP)**: Smart auto-selection with locked account visibility and swapping
- **Financial Impact**: $7,800/year cost savings + $1,800/year revenue = $9,600/year benefit
- **Timeline**: 4 weeks
- **Complexity**: Medium

**Phase 2 (Advanced)**: User selection UI during link flow
- **Timeline**: 2 weeks (after Plaid research)
- **Complexity**: Medium-High

**Key Benefits**:
- ✅ Enforces subscription tier limits (currently not enforced)
- ✅ Reduces Plaid API costs (only sync active accounts)
- ✅ Creates upgrade pressure (locked accounts visible with CTA)
- ✅ Maintains user control (swapping + future selection UI)
- ✅ Grandfathers existing users (no disruption)
- ✅ Future-ready for multi-provider support

**Next Steps**:
1. Review and approve this design
2. Create GitHub issue #237
3. Begin Phase 1 implementation (Week 1: Database migration)

---

**Document Version**: 1.0
**Last Updated**: 2025-01-25
**Owner**: @maxnussbaum
**Status**: Pending Approval
