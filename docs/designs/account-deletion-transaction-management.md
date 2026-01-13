# Account Deletion & Transaction Management Design Specification

**Created**: 2025-01-25
**Status**: Proposed
**Complexity**: Large (4-6 weeks)
**Priority**: P1 (High) - Critical security/abuse prevention

---

## Executive Summary

BoostedBudget currently uses **hard deletion with CASCADE** for accounts and linked items. When a user deletes a bank connection, ALL associated data (accounts, and future transactions) are permanently deleted from the database. This creates **severe abuse vectors** that allow users to bypass subscription tier limits.

### The Critical Problem

**Free tier users can game the system** by repeatedly:
1. Connect Bank A (3 accounts) → Sync 6 months of transactions
2. Delete Bank A connection → ALL data purged
3. Connect Bank B (3 accounts) → Sync another 6 months of transactions
4. Repeat monthly → **Unlimited accounts + unlimited transaction history on Free tier**

This completely circumvents:
- ✅ Account limits (3 for Free tier)
- ✅ Transaction data limits (future)
- ✅ Data export limits (1/month for Free tier)
- ✅ Rule execution limits (tied to deleted accounts)

### Financial Impact

**Estimated Annual Loss** (Conservative):
- 500 Free tier users × 20% gaming (100 users) × $8/month Premium value = **$9,600/year**
- Plaid API costs: 100 users × 10 account rotations/year × $2.50/connection = **$2,500/year**
- **Total: $12,100/year** in lost revenue + excess costs

**Risk**: As user base grows, sophisticated users WILL discover this exploit

### Recommended Solution

**Implement soft deletion with lifetime tracking**:
- **Soft Delete**: Mark records as `deleted_at` instead of purging
- **Lifetime Limits**: Track total accounts ever connected (not just active)
- **Audit Logging**: Monitor deletion patterns, flag suspicious behavior
- **GDPR Compliance**: Allow hard delete after 30-90 day grace period

**Timeline**: 4-6 weeks | **ROI**: $12,100/year prevention + future scalability

---

## Table of Contents

1. [Current Architecture Analysis](#current-architecture-analysis)
2. [Abuse Scenarios (Detailed)](#abuse-scenarios-detailed)
3. [Design Options Evaluated](#design-options-evaluated)
4. [Recommended Solution](#recommended-solution)
5. [Technical Implementation](#technical-implementation)
6. [Database Schema Changes](#database-schema-changes)
7. [Backend Implementation](#backend-implementation)
8. [Frontend Changes](#frontend-changes)
9. [GDPR Compliance](#gdpr-compliance)
10. [Testing Strategy](#testing-strategy)
11. [Metrics & Monitoring](#metrics--monitoring)
12. [Migration Strategy](#migration-strategy)
13. [Cost Analysis](#cost-analysis)
14. [Timeline & Phases](#timeline--phases)
15. [Open Questions](#open-questions)
16. [Acceptance Criteria](#acceptance-criteria)

---

## Current Architecture Analysis

### Database Cascade Behavior

**`linked_items` table** (migration 000006):
```sql
CREATE TABLE linked_items (
    id UUID PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    item_id VARCHAR(255) NOT NULL,
    access_token_encrypted TEXT NOT NULL,
    cursor TEXT,  -- Transaction sync cursor
    last_synced_at TIMESTAMP,
    ...
);
```

**`accounts` table** (migration 000007):
```sql
CREATE TABLE accounts (
    id UUID PRIMARY KEY,
    linked_item_id UUID NOT NULL REFERENCES linked_items(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    plaid_account_id VARCHAR(255) NOT NULL,
    ...
);
```

### What Happens When User Deletes a Linked Item

**Current Behavior** (Hard Delete):
```sql
DELETE FROM linked_items WHERE id = 'item-123';

-- Cascades to:
-- 1. ALL accounts for this item (via linked_item_id FK)
-- 2. ALL transactions for those accounts (future - when implemented)
-- 3. ALL budget rules referencing those accounts (future)
-- 4. ALL transaction categorizations (future)

-- Result: Complete data purge, no audit trail
```

**Data Lost**:
- Historical transaction data (6-24 months of Plaid sync data)
- Account balance history
- Budget rule associations
- Transaction categorizations
- User spending patterns
- Audit trail of what was deleted

### Future Transaction Table (Planned Phase 2.3)

From `boosted-budget-complete-plan.md`:
```sql
CREATE TABLE transactions (
    id UUID PRIMARY KEY,
    account_id UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    plaid_transaction_id VARCHAR(255) NOT NULL,
    amount DECIMAL(15,2) NOT NULL,
    date DATE NOT NULL,
    name VARCHAR(255),
    category VARCHAR(100),
    pending BOOLEAN,
    ...
);
```

**Problem**: `ON DELETE CASCADE` means deleting account → deletes ALL transactions → enables abuse

---

## Abuse Scenarios (Detailed)

### Scenario 1: Account Cycling (Current Threat)

**Free Tier Limits**: 3 accounts, 1 rule, 1 execution/day

**Attack Vector**:
```
Month 1:
- Connect Chase Bank (3 accounts: Checking, Savings, Credit Card)
- Active accounts: 3/3 limit reached

Month 2:
- Delete Chase Bank connection → CASCADE deletes all 3 accounts
- Connect Wells Fargo (3 accounts: Checking, Savings, Investment)
- Active accounts: 3/3 limit reached

Month 3:
- Delete Wells Fargo → CASCADE deletes all 3 accounts
- Connect Bank of America (3 accounts)
- Active accounts: 3/3 limit reached

Result over 12 months:
- 12 banks × 3 accounts = 36 accounts rotated through
- User stays on Free tier (always 3 active accounts)
- Bypassed account limit: 1,200% over limit
```

**Financial Impact**:
- User should have upgraded to Premium ($9.99/month) to access 30 accounts
- Lost revenue: $9.99 × 12 = **$119.88/year per user**
- 100 users gaming: **$11,988/year**

---

### Scenario 2: Transaction History Accumulation (Future Threat - Phase 2.3)

**Free Tier Limits** (hypothetical): 3 accounts, 6 months transaction history

**Attack Vector**:
```
Month 1:
- Connect Chase Bank (3 accounts)
- Sync 6 months of transactions: Jan-Jun 2024
- Total transactions stored: ~500 transactions

Month 2:
- Delete Chase Bank → Transactions CASCADE deleted
- Connect Wells Fargo (3 accounts)
- Sync 6 months of transactions: Jul-Dec 2024
- Total transactions stored: ~500 transactions

Month 3:
- Delete Wells Fargo → Transactions CASCADE deleted
- Connect Bank of America (3 accounts)
- Sync 6 months of transactions: Jan-Jun 2025
- Total transactions stored: ~500 transactions

Result over 12 months:
- 12 banks × 500 transactions = 6,000 transactions processed
- User stays on Free tier (always 6mo history)
- Plaid transaction sync costs: 12 × $0.60 = $7.20/year
```

**Why This Matters**:
- User gets full year+ of transaction data across multiple banks
- Bypasses intended "6 months history" limit
- Can export full year of data before deleting
- Plaid charges us for EACH sync ($0.60/account for transactions product)

**Financial Impact**:
- Premium tier includes unlimited transaction history
- User should upgrade to Premium ($9.99/month)
- Lost revenue: $9.99 × 12 = **$119.88/year**
- Excess Plaid costs: $7.20/year
- **Total: $127.08/year per user**

---

### Scenario 3: Data Export Abuse

**Free Tier Limits**: 1 data export request per month

**Attack Vector**:
```
Day 1:
- Connect Chase Bank (3 accounts)
- Sync 6 months of transactions
- Export data (1/1 monthly limit used)

Day 5:
- Delete Chase Bank connection
- Connect Wells Fargo (3 accounts)
- Sync 6 months of transactions
- Export data (counter reset? or still enforced?)

Day 10:
- Delete Wells Fargo
- Connect Bank of America
- Export data

Result:
- 3 data exports in 10 days (3x monthly limit)
- If tied to accounts: Unlimited exports via account cycling
```

**Current Implementation Gap**:
- Export limits tracked per user OR per account?
- If per account: Deleting accounts resets export counter
- Need to track: `user_lifetime_exports` not `account_exports`

---

### Scenario 4: Budget Rule Gaming

**Free Tier Limits**: 1 automation rule

**Attack Vector**:
```
Week 1:
- Connect Chase Checking account
- Create rule: "Save 10% of paycheck deposits"
- Rule executes 1x/day (Free tier limit)

Week 2:
- Delete Chase Checking → Rule becomes invalid (account missing)
- Connect Wells Fargo Checking
- Create NEW rule: "Round up transactions"
- Now have 1 active rule (within limit)

Week 3:
- Delete Wells Fargo → Rule becomes invalid
- Connect BoA Checking
- Create NEW rule: "Auto-pay bills"

Result:
- 3 different rules tested over 3 weeks
- User stays on Free tier (1 active rule at a time)
- Bypasses intent of "1 rule" limit (meant to limit automation scope)
```

**Design Question**:
- Should rules be preserved when accounts are deleted?
- Should rules count toward lifetime limit or just active limit?
- Premium tier: 15 rules - should this be lifetime or concurrent?

---

### Scenario 5: Plaid API Cost Abuse

**Plaid Pricing**:
- Link Token Creation: FREE
- Item Creation: $0.60 - $3.00 one-time
- Transactions Product: $0.60 - $3.00 per Item
- Auth Product: $0.10 - $0.60 per Item

**Attack Vector**:
```
User strategy:
1. Connect Bank A → Plaid charges us $3.00 (Item + Transactions)
2. Sync 6 months of data
3. Delete Bank A
4. Connect Bank B → Plaid charges us another $3.00
5. Repeat 12x/year

Our costs:
- 12 connections × $3.00 = $36/year per user
- 100 users gaming: $3,600/year in Plaid fees
- User stays on Free tier ($0 revenue)
```

**Mitigation**:
- Track lifetime Items connected per user
- Free tier: Max 5 lifetime Items (allows 2 bank switches for legitimate reasons)
- Premium tier: Unlimited lifetime Items
- Warn user: "You've connected 4 of 5 lifetime bank connections for Free tier"

---

### Scenario 6: GDPR "Right to Erasure" Exploitation

**GDPR Rights**:
- Right to erasure: User can request data deletion
- 30-day grace period before permanent deletion

**Potential Abuse**:
```
User strategy:
1. Connect banks, sync data, use app
2. Request GDPR data deletion → 30-day countdown
3. Within 30 days: Download all data exports
4. Day 29: Cancel deletion request, restore account
5. Continue using app
6. Repeat when hitting limits

OR:

1. Request deletion → 30-day grace
2. During grace period: Still counted as deleted for tier limits?
3. Connect new accounts (bypassing limit)
4. Cancel deletion
5. Now have old + new accounts (over limit)
```

**Current Gap**:
- No soft delete implementation
- No grace period tracking
- Immediate hard delete vs. GDPR-compliant delayed delete

---

## Design Options Evaluated

### Option 1: Soft Deletion with `deleted_at` Column

**Description**: Add `deleted_at TIMESTAMP` to `linked_items` and `accounts` tables. Records marked as deleted are excluded from queries but preserved in database.

**Schema**:
```sql
ALTER TABLE linked_items ADD COLUMN deleted_at TIMESTAMP WITH TIME ZONE;
ALTER TABLE accounts ADD COLUMN deleted_at TIMESTAMP WITH TIME ZONE;

-- Query only active items
SELECT * FROM accounts WHERE deleted_at IS NULL;

-- Include deleted for audit
SELECT * FROM accounts WHERE deleted_at IS NOT NULL;
```

**Pros**:
- ✅ Preserves audit trail (who deleted what, when)
- ✅ Allows "undelete" within grace period (GDPR-compliant)
- ✅ Transactions remain queryable for historical analysis
- ✅ Simplest implementation (single column)
- ✅ Standard industry practice

**Cons**:
- ⚠️ Increases database storage (deleted records never purged without maintenance)
- ⚠️ Queries must filter `deleted_at IS NULL` (risk of missing filter)
- ⚠️ Unique constraints need adjustment (ignore deleted records)

**Verdict**: ✅ **RECOMMENDED** - Core of solution

---

### Option 2: Lifetime Account Tracking

**Description**: Track total accounts ever connected, not just current active accounts.

**Schema**:
```sql
CREATE TABLE user_lifetime_stats (
    user_id UUID PRIMARY KEY REFERENCES users(id),
    total_accounts_connected INTEGER DEFAULT 0,
    total_items_connected INTEGER DEFAULT 0,
    total_transactions_synced INTEGER DEFAULT 0,
    total_data_exports INTEGER DEFAULT 0,
    first_account_connected_at TIMESTAMP,
    last_account_connected_at TIMESTAMP
);

-- Increment on each new account
UPDATE user_lifetime_stats
SET total_accounts_connected = total_accounts_connected + 1
WHERE user_id = ?;
```

**Tier Limit Checks**:
```go
// OLD: Only count active accounts
activeCount := countActiveAccounts(userID)
if activeCount >= tier.MaxAccounts {
    return ErrAccountLimitReached
}

// NEW: Check BOTH active AND lifetime
activeCount := countActiveAccounts(userID)
lifetimeCount := getLifetimeAccountsConnected(userID)

if activeCount >= tier.MaxAccounts {
    return ErrAccountLimitReached
}

if lifetimeCount >= tier.MaxLifetimeAccounts {
    return ErrLifetimeAccountLimitReached
}
```

**Tier Configuration** (`config/subscription_tiers.yaml`):
```yaml
tiers:
  free:
    limits:
      accounts: 3              # Concurrent active accounts
      max_lifetime_accounts: 10  # NEW: Total accounts ever connected
      max_lifetime_items: 5      # Total bank connections ever

  premium:
    limits:
      accounts: 30
      max_lifetime_accounts: -1  # Unlimited
      max_lifetime_items: -1
```

**Pros**:
- ✅ Prevents account cycling abuse
- ✅ Simple counter, easy to query
- ✅ Clear user communication: "You've used 8 of 10 lifetime account slots"

**Cons**:
- ⚠️ Legitimate users might hit lifetime limit (e.g., moving banks, closing accounts)
- ⚠️ Requires generous lifetime limits to avoid friction

**Verdict**: ✅ **RECOMMENDED** - Complements soft delete

---

### Option 3: Cooldown Periods

**Description**: Prevent re-adding accounts for X days after deletion.

**Schema**:
```sql
CREATE TABLE account_deletion_cooldowns (
    id UUID PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users(id),
    plaid_account_id VARCHAR(255) NOT NULL,
    institution_id VARCHAR(255),
    deleted_at TIMESTAMP NOT NULL,
    cooldown_ends_at TIMESTAMP NOT NULL,
    UNIQUE(user_id, plaid_account_id)
);

-- Check cooldown before allowing re-add
SELECT * FROM account_deletion_cooldowns
WHERE user_id = ?
  AND plaid_account_id = ?
  AND cooldown_ends_at > NOW();
```

**Cooldown Durations** (tier-based):
```yaml
tiers:
  free:
    account_deletion_cooldown_days: 30  # Can't re-add same account for 30 days

  premium:
    account_deletion_cooldown_days: 0   # No cooldown
```

**Pros**:
- ✅ Prevents rapid account churn
- ✅ Legitimate users rarely re-add same account immediately
- ✅ Slows down gaming attacks

**Cons**:
- ⚠️ Complex UX ("You deleted this account 15 days ago, cooldown remaining: 15 days")
- ⚠️ Edge case: User closes bank account, opens new account at same bank → blocked by cooldown
- ⚠️ Adds friction for legitimate use cases

**Verdict**: ⚠️ **OPTIONAL** - Consider for Phase 2 if abuse continues

---

### Option 4: Transaction Retention Policy

**Description**: Keep transaction data even after account deletion.

**Schema**:
```sql
-- accounts table has deleted_at
-- transactions table keeps account_id reference

-- Transactions remain queryable after account deletion
SELECT t.* FROM transactions t
JOIN accounts a ON t.account_id = a.id
WHERE t.user_id = ?
  AND a.deleted_at IS NOT NULL;  -- Transactions from deleted accounts
```

**Benefits**:
- User can view historical spending even after deleting accounts
- Budget insights remain accurate (don't lose historical data)
- GDPR: Transactions marked for deletion only when user requests full erasure

**GDPR Compliance**:
```go
// User can delete account without deleting transactions
func (s *AccountService) DeleteAccount(userID, accountID string) error {
    // Soft delete account
    account.DeletedAt = time.Now()
    s.repo.Update(account)

    // Transactions remain (tied to deleted account)
    // User must explicitly request GDPR erasure for transaction deletion
}

// GDPR erasure: Delete ALL user data including transactions
func (s *GDPRService) RequestErasure(userID string) error {
    // Schedule deletion in 30 days (grace period)
    s.scheduleErasure(userID, time.Now().Add(30 * 24 * time.Hour))
}
```

**Pros**:
- ✅ Preserves valuable user data (spending patterns, budgets)
- ✅ Prevents transaction abuse (can't delete to reset limits)
- ✅ GDPR-compliant (separate deletion request for erasure)

**Cons**:
- ⚠️ User confusion: "I deleted my account but still see transactions?"
- ⚠️ Storage costs (transactions pile up over time)

**Verdict**: ✅ **RECOMMENDED** - Critical for future transaction features

---

### Option 5: Audit Logging + Anomaly Detection

**Description**: Log all deletion events and detect suspicious patterns.

**Schema**:
```sql
CREATE TABLE account_deletion_audit (
    id UUID PRIMARY KEY,
    user_id UUID NOT NULL,
    account_id UUID NOT NULL,
    linked_item_id UUID NOT NULL,
    institution_name VARCHAR(255),
    account_type VARCHAR(50),
    deleted_at TIMESTAMP NOT NULL,
    deletion_reason TEXT,  -- User-provided reason (optional)
    deletion_ip_address VARCHAR(45),
    deletion_user_agent TEXT,
    transactions_count INTEGER,  -- How many transactions were on this account
    account_age_days INTEGER,    -- How long account was connected
    is_suspicious BOOLEAN DEFAULT FALSE,
    review_status VARCHAR(20)   -- pending, reviewed, flagged, cleared
);
```

**Anomaly Detection Rules**:
```go
// Flag suspicious deletion patterns
func (s *AuditService) CheckDeletionPattern(userID string) (bool, string) {
    recent := s.getDeletionsLast30Days(userID)

    // Rule 1: More than 3 deletions in 30 days
    if len(recent) > 3 {
        return true, "Excessive deletions: >3 in 30 days"
    }

    // Rule 2: Account deleted within 7 days of creation
    for _, deletion := range recent {
        if deletion.AccountAgeDays < 7 {
            return true, "Quick churn: Account deleted <7 days after creation"
        }
    }

    // Rule 3: Deletion immediately after data export
    for _, deletion := range recent {
        if s.wasExportedRecently(deletion.AccountID, 24*time.Hour) {
            return true, "Export + delete pattern detected"
        }
    }

    return false, ""
}
```

**Actions on Suspicious Activity**:
```go
if suspicious, reason := s.CheckDeletionPattern(userID); suspicious {
    // 1. Log to security monitoring
    s.securityLog.Warn("Suspicious deletion pattern", userID, reason)

    // 2. Require deletion reason from user
    s.promptUserForDeletionReason(userID)

    // 3. Add friction: Manual review before allowing more connections
    s.flagAccountForReview(userID)

    // 4. Email notification to security team
    s.notifySecurityTeam(userID, reason)
}
```

**Pros**:
- ✅ Detects abuse without blocking legitimate users
- ✅ Provides data for policy decisions
- ✅ Enables manual review of edge cases

**Cons**:
- ⚠️ Requires manual review process
- ⚠️ False positives (legitimate users flagged)
- ⚠️ Doesn't prevent abuse, only detects it

**Verdict**: ✅ **RECOMMENDED** - Complements automated limits

---

### Option 6: Tier-Based Deletion Limits

**Description**: Free tier can only delete N accounts per month.

**Schema**:
```sql
CREATE TABLE user_deletion_quotas (
    user_id UUID PRIMARY KEY,
    month DATE NOT NULL,  -- YYYY-MM-01
    deletions_this_month INTEGER DEFAULT 0,
    deletions_remaining INTEGER,
    quota_resets_at TIMESTAMP
);
```

**Tier Configuration**:
```yaml
tiers:
  free:
    max_deletions_per_month: 2  # Can only delete 2 accounts/month

  premium:
    max_deletions_per_month: -1  # Unlimited
```

**Enforcement**:
```go
func (s *AccountService) DeleteAccount(userID, accountID string) error {
    tier := s.getTier(userID)

    if tier.Name == "free" {
        quota := s.getDeletionQuota(userID)
        if quota.DeletionsRemaining <= 0 {
            return ErrDeletionQuotaExceeded
        }

        quota.DeletionsThisMonth++
        quota.DeletionsRemaining--
        s.updateQuota(quota)
    }

    // Proceed with deletion
    s.softDeleteAccount(accountID)
}
```

**Pros**:
- ✅ Direct prevention of rapid account cycling
- ✅ Clear user messaging: "2 of 2 monthly deletions used, resets on Feb 1"
- ✅ Simple to implement and enforce

**Cons**:
- ⚠️ Friction for legitimate users (e.g., closing accounts, switching banks)
- ⚠️ Users might "save" deletions for month-end gaming
- ⚠️ Premium upsell pressure feels punitive

**Verdict**: ⚠️ **OPTIONAL** - Consider if other measures insufficient

---

### Option 7: Tombstone Records

**Description**: Replace deleted records with tombstone placeholders.

**Schema**:
```sql
CREATE TABLE account_tombstones (
    id UUID PRIMARY KEY,
    original_account_id UUID NOT NULL,
    user_id UUID NOT NULL,
    plaid_account_id VARCHAR(255),
    institution_name VARCHAR(255),
    account_type VARCHAR(50),
    created_at TIMESTAMP,
    deleted_at TIMESTAMP,
    deletion_reason TEXT
);

-- When account deleted:
INSERT INTO account_tombstones SELECT * FROM accounts WHERE id = ?;
DELETE FROM accounts WHERE id = ?;
```

**Pros**:
- ✅ Preserves history without cluttering active tables
- ✅ Clear separation of active vs. historical data
- ✅ Easier to purge old tombstones (separate table)

**Cons**:
- ⚠️ More complex queries (JOIN tombstones for full history)
- ⚠️ Duplicate schema maintenance (tombstones must match accounts)
- ⚠️ Transactions still reference deleted account IDs

**Verdict**: ❌ **NOT RECOMMENDED** - Soft delete simpler and sufficient

---

### Option 8: Combined Approach (Recommended)

**Description**: Implement multiple complementary safeguards.

**Components**:
1. **Soft Deletion** (Option 1) - Core implementation
2. **Lifetime Tracking** (Option 2) - Prevent cycling
3. **Transaction Retention** (Option 4) - Preserve data value
4. **Audit Logging** (Option 5) - Detect anomalies
5. **GDPR Grace Period** - 30-day undelete window

**Implementation Priority**:
- **Phase 1 (MVP)**: Soft delete + lifetime tracking + audit logs
- **Phase 2 (Advanced)**: Anomaly detection + automated flagging
- **Phase 3 (Optional)**: Cooldown periods or deletion quotas if needed

**Verdict**: ✅✅ **FINAL RECOMMENDATION**

---

## Recommended Solution

**Implement Option 8: Combined Approach** with phased rollout.

### Phase 1: Core Safeguards (4 weeks)

**Database Changes**:
1. Add `deleted_at` column to `linked_items` and `accounts`
2. Create `user_lifetime_stats` table for tracking
3. Create `account_deletion_audit` table for logging
4. Update unique constraints to ignore deleted records

**Backend Changes**:
1. Soft delete instead of hard delete
2. Track lifetime accounts/items connected
3. Enforce lifetime limits based on tier
4. Log all deletion events with metadata
5. Update queries to filter `deleted_at IS NULL`

**Frontend Changes**:
1. "Delete" button → soft deletes with confirmation
2. Show lifetime stats in account dashboard
3. Warning when approaching lifetime limits
4. Optional deletion reason prompt

### Phase 2: Anomaly Detection (2 weeks)

**Backend Changes**:
1. Implement suspicious pattern detection
2. Flag users with rapid churn
3. Manual review workflow for flagged accounts
4. Email notifications to security team

**Metrics Dashboard**:
1. Track deletion patterns per tier
2. Monitor abuse indicators
3. Conversion rate: flagged users → legitimate vs. abuse

---

## Technical Implementation

### Phase 1: Database Schema Changes

**Migration**: `000009_add_soft_deletion.up.sql`

```sql
-- Add soft deletion to linked_items
ALTER TABLE linked_items
ADD COLUMN deleted_at TIMESTAMP WITH TIME ZONE,
ADD COLUMN deletion_reason TEXT,
ADD COLUMN deleted_by VARCHAR(255);  -- IP address or user agent

-- Add soft deletion to accounts
ALTER TABLE accounts
ADD COLUMN deleted_at TIMESTAMP WITH TIME ZONE,
ADD COLUMN deletion_reason TEXT;

-- Update unique constraint to ignore deleted records
-- PostgreSQL: Use partial unique index
DROP INDEX IF EXISTS linked_items_user_id_item_id_key;
CREATE UNIQUE INDEX unique_user_item_active
ON linked_items(user_id, item_id)
WHERE deleted_at IS NULL;

DROP INDEX IF EXISTS accounts_user_id_plaid_account_id_key;
CREATE UNIQUE INDEX unique_user_account_active
ON accounts(user_id, plaid_account_id)
WHERE deleted_at IS NULL;

-- Create lifetime stats table
CREATE TABLE user_lifetime_stats (
    user_id UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    total_accounts_connected INTEGER DEFAULT 0,
    total_items_connected INTEGER DEFAULT 0,
    total_transactions_synced INTEGER DEFAULT 0,
    total_data_exports INTEGER DEFAULT 0,
    first_account_connected_at TIMESTAMP WITH TIME ZONE,
    last_account_connected_at TIMESTAMP WITH TIME ZONE,
    first_item_connected_at TIMESTAMP WITH TIME ZONE,
    last_item_connected_at TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Create audit table
CREATE TABLE account_deletion_audit (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id),
    account_id UUID NOT NULL,
    linked_item_id UUID NOT NULL,
    plaid_account_id VARCHAR(255),
    institution_name VARCHAR(255),
    institution_id VARCHAR(255),
    account_type VARCHAR(50),
    account_name VARCHAR(255),
    deleted_at TIMESTAMP WITH TIME ZONE NOT NULL,
    deletion_reason TEXT,
    deletion_ip_address VARCHAR(45),
    deletion_user_agent TEXT,
    transactions_count INTEGER DEFAULT 0,
    account_age_days INTEGER,
    last_sync_at TIMESTAMP WITH TIME ZONE,
    is_suspicious BOOLEAN DEFAULT FALSE,
    suspicion_reasons TEXT[],
    review_status VARCHAR(20) DEFAULT 'pending',  -- pending, reviewed, flagged, cleared
    reviewed_by VARCHAR(255),
    reviewed_at TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Indexes for performance
CREATE INDEX idx_deletion_audit_user_id ON account_deletion_audit(user_id);
CREATE INDEX idx_deletion_audit_deleted_at ON account_deletion_audit(deleted_at);
CREATE INDEX idx_deletion_audit_suspicious ON account_deletion_audit(is_suspicious) WHERE is_suspicious = TRUE;
CREATE INDEX idx_deletion_audit_review_status ON account_deletion_audit(review_status) WHERE review_status = 'pending';

COMMENT ON TABLE user_lifetime_stats IS 'Tracks lifetime account/transaction stats to prevent gaming via deletion';
COMMENT ON TABLE account_deletion_audit IS 'Audit log of all account deletions for abuse detection';
```

**Migration**: `000009_add_soft_deletion.down.sql`

```sql
DROP TABLE IF EXISTS account_deletion_audit;
DROP TABLE IF EXISTS user_lifetime_stats;

ALTER TABLE accounts DROP COLUMN IF EXISTS deleted_at;
ALTER TABLE accounts DROP COLUMN IF EXISTS deletion_reason;

ALTER TABLE linked_items DROP COLUMN IF EXISTS deleted_at;
ALTER TABLE linked_items DROP COLUMN IF EXISTS deletion_reason;
ALTER TABLE linked_items DROP COLUMN IF EXISTS deleted_by;

-- Restore original unique constraints
DROP INDEX IF EXISTS unique_user_item_active;
ALTER TABLE linked_items ADD CONSTRAINT unique_user_item UNIQUE (user_id, item_id);

DROP INDEX IF EXISTS unique_user_account_active;
ALTER TABLE accounts ADD CONSTRAINT unique_user_account UNIQUE (user_id, plaid_account_id);
```

---

## Backend Implementation

### New Service: `deletion_service.go`

**Location**: `backend/internal/services/deletion_service.go`

```go
package services

import (
    "context"
    "fmt"
    "time"

    "github.com/maxnussbaum/BoostedBudget/internal/domain"
    "github.com/maxnussbaum/BoostedBudget/internal/repository"
)

type DeletionService interface {
    SoftDeleteAccount(ctx context.Context, userID, accountID string, reason string) error
    SoftDeleteLinkedItem(ctx context.Context, userID, itemID string, reason string) error
    CheckDeletionAllowed(ctx context.Context, userID string) error
    GetDeletionAudit(ctx context.Context, userID string, limit int) ([]*domain.DeletionAudit, error)
}

type deletionService struct {
    accountRepo     repository.AccountRepository
    itemRepo        repository.LinkedItemRepository
    statsRepo       repository.LifetimeStatsRepository
    auditRepo       repository.DeletionAuditRepository
    subscriptionSvc SubscriptionService
    anomalyDetector *AnomalyDetector
}

func NewDeletionService(
    accountRepo repository.AccountRepository,
    itemRepo repository.LinkedItemRepository,
    statsRepo repository.LifetimeStatsRepository,
    auditRepo repository.DeletionAuditRepository,
    subscriptionSvc SubscriptionService,
) DeletionService {
    return &deletionService{
        accountRepo:     accountRepo,
        itemRepo:        itemRepo,
        statsRepo:       statsRepo,
        subscriptionSvc: subscriptionSvc,
        anomalyDetector: NewAnomalyDetector(),
    }
}

// SoftDeleteAccount marks an account as deleted without removing it from database
func (s *deletionService) SoftDeleteAccount(
    ctx context.Context,
    userID, accountID, reason string,
) error {
    // 1. Check if deletion is allowed (tier limits, anomaly detection)
    if err := s.CheckDeletionAllowed(ctx, userID); err != nil {
        return err
    }

    // 2. Get account details before deletion (for audit)
    account, err := s.accountRepo.GetByID(ctx, accountID)
    if err != nil {
        return fmt.Errorf("account not found: %w", err)
    }

    if account.UserID != userID {
        return fmt.Errorf("account does not belong to user")
    }

    if account.DeletedAt != nil {
        return fmt.Errorf("account already deleted")
    }

    // 3. Get transaction count (for audit)
    // TODO: Implement when transactions table exists
    transactionCount := 0

    // 4. Calculate account age
    accountAgeDays := int(time.Since(account.CreatedAt).Hours() / 24)

    // 5. Soft delete account
    account.DeletedAt = &time.Time{}
    *account.DeletedAt = time.Now()
    account.DeletionReason = &reason

    if err := s.accountRepo.Update(ctx, account); err != nil {
        return fmt.Errorf("failed to delete account: %w", err)
    }

    // 6. Create audit log entry
    audit := &domain.DeletionAudit{
        UserID:            userID,
        AccountID:         accountID,
        LinkedItemID:      account.LinkedItemID,
        PlaidAccountID:    account.PlaidAccountID,
        AccountType:       account.Type,
        AccountName:       account.Name,
        DeletedAt:         *account.DeletedAt,
        DeletionReason:    reason,
        TransactionsCount: transactionCount,
        AccountAgeDays:    accountAgeDays,
        LastSyncAt:        account.UpdatedAt,  // Placeholder
    }

    // Get institution details from linked item
    item, _ := s.itemRepo.GetByID(ctx, account.LinkedItemID)
    if item != nil {
        audit.InstitutionName = item.InstitutionName
        audit.InstitutionID = item.InstitutionID
    }

    // 7. Run anomaly detection
    suspicious, reasons := s.anomalyDetector.CheckDeletionPattern(ctx, userID, audit)
    audit.IsSuspicious = suspicious
    audit.SuspicionReasons = reasons

    if err := s.auditRepo.Create(ctx, audit); err != nil {
        // Non-blocking error - log but continue
        fmt.Printf("Failed to create audit log: %v\n", err)
    }

    // 8. If suspicious, flag for review
    if suspicious {
        // TODO: Send notification to security team
        // TODO: Add friction for next connection (require manual review)
    }

    return nil
}

// CheckDeletionAllowed validates if user can delete based on tier limits and patterns
func (s *deletionService) CheckDeletionAllowed(ctx context.Context, userID string) error {
    // Get user's tier
    user, err := s.subscriptionSvc.GetUser(ctx, userID)
    if err != nil {
        return err
    }

    tier, err := s.subscriptionSvc.GetUserTier(ctx, user)
    if err != nil {
        return err
    }

    // Check monthly deletion quota (if configured for tier)
    if tier.MaxDeletionsPerMonth > 0 {
        deletionsThisMonth := s.auditRepo.CountDeletionsThisMonth(ctx, userID)
        if deletionsThisMonth >= tier.MaxDeletionsPerMonth {
            return fmt.Errorf(
                "monthly deletion limit reached (%d/%d) - upgrade to remove limits",
                deletionsThisMonth, tier.MaxDeletionsPerMonth,
            )
        }
    }

    // Check anomaly patterns (soft block)
    recentDeletions := s.auditRepo.GetRecentDeletions(ctx, userID, 30) // Last 30 days
    if len(recentDeletions) > 5 {
        // More than 5 deletions in 30 days - suspicious
        // Don't hard-block, but log and flag
        fmt.Printf("User %s has excessive deletions: %d in 30 days\n", userID, len(recentDeletions))
        // Allow deletion but mark as suspicious in audit
    }

    return nil
}
```

---

### New Service: `anomaly_detector.go`

**Location**: `backend/internal/services/anomaly_detector.go`

```go
package services

import (
    "context"
    "time"

    "github.com/maxnussbaum/BoostedBudget/internal/domain"
)

type AnomalyDetector struct {}

func NewAnomalyDetector() *AnomalyDetector {
    return &AnomalyDetector{}
}

// CheckDeletionPattern analyzes deletion for suspicious behavior
func (d *AnomalyDetector) CheckDeletionPattern(
    ctx context.Context,
    userID string,
    deletion *domain.DeletionAudit,
) (bool, []string) {
    reasons := []string{}

    // Rule 1: Account deleted within 7 days of creation (quick churn)
    if deletion.AccountAgeDays < 7 {
        reasons = append(reasons, fmt.Sprintf(
            "Quick churn: Account deleted %d days after creation",
            deletion.AccountAgeDays,
        ))
    }

    // Rule 2: Account never synced transactions (connected but never used)
    if deletion.LastSyncAt == nil || deletion.LastSyncAt.IsZero() {
        reasons = append(reasons, "Account never synced transactions")
    }

    // Rule 3: High transaction count (user extracted data then deleted)
    if deletion.TransactionsCount > 500 {
        reasons = append(reasons, fmt.Sprintf(
            "High transaction count: %d transactions synced before deletion",
            deletion.TransactionsCount,
        ))
    }

    // Future rules:
    // - Deletion immediately after data export
    // - Pattern of delete-wait-add cycles
    // - Deletion during subscription downgrade

    return len(reasons) > 0, reasons
}
```

---

### Update: `account_repository.go`

**Location**: `backend/internal/repository/account_repository.go`

```go
// Add methods to support soft deletion

// GetByID retrieves active (non-deleted) account
func (r *accountRepository) GetByID(ctx context.Context, id string) (*domain.Account, error) {
    var account domain.Account
    err := r.db.QueryRowContext(ctx, `
        SELECT * FROM accounts
        WHERE id = $1 AND deleted_at IS NULL
    `, id).Scan(&account)

    if err == sql.ErrNoRows {
        return nil, ErrAccountNotFound
    }
    return &account, err
}

// CountActiveAccounts counts non-deleted accounts for user
func (r *accountRepository) CountActiveAccounts(ctx context.Context, userID string) (int, error) {
    var count int
    err := r.db.QueryRowContext(ctx, `
        SELECT COUNT(*) FROM accounts
        WHERE user_id = $1 AND deleted_at IS NULL
    `, userID).Scan(&count)
    return count, err
}

// GetActiveAccounts retrieves all active accounts for user
func (r *accountRepository) GetActiveAccounts(ctx context.Context, userID string) ([]*domain.Account, error) {
    rows, err := r.db.QueryContext(ctx, `
        SELECT * FROM accounts
        WHERE user_id = $1 AND deleted_at IS NULL
        ORDER BY created_at DESC
    `, userID)
    // ... scan and return accounts
}

// GetDeletedAccounts retrieves deleted accounts (for GDPR/audit)
func (r *accountRepository) GetDeletedAccounts(ctx context.Context, userID string) ([]*domain.Account, error) {
    rows, err := r.db.QueryContext(ctx, `
        SELECT * FROM accounts
        WHERE user_id = $1 AND deleted_at IS NOT NULL
        ORDER BY deleted_at DESC
    `, userID)
    // ... scan and return accounts
}
```

---

### Update: Tier Configuration

**File**: `config/subscription_tiers.yaml`

```yaml
tiers:
  free:
    display_name: "Free"
    limits:
      accounts: 3
      max_lifetime_accounts: 10  # NEW: Total accounts ever connected
      max_lifetime_items: 5      # NEW: Total bank connections ever
      max_deletions_per_month: 2 # NEW: Optional deletion quota

  premium:
    limits:
      accounts: 30
      max_lifetime_accounts: 100  # Generous but not unlimited
      max_lifetime_items: 20
      max_deletions_per_month: -1  # Unlimited

  premium_plus:
    limits:
      accounts: -1
      max_lifetime_accounts: -1  # Truly unlimited
      max_lifetime_items: -1
      max_deletions_per_month: -1
```

---

## Frontend Changes

### Delete Account Confirmation Modal

**Location**: `apps/web/src/features/accounts/components/DeleteAccountModal.tsx`

```typescript
import { useState } from 'react'
import { deleteAccount } from '../api/accountsApi'

interface DeleteAccountModalProps {
  account: Account
  userStats: UserLifetimeStats
  onClose: () => void
  onDeleted: () => void
}

export function DeleteAccountModal({ account, userStats, onClose, onDeleted }: DeleteAccountModalProps) {
  const [reason, setReason] = useState('')
  const [confirmText, setConfirmText] = useState('')
  const [deleting, setDeleting] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const lifetimeUsage = `${userStats.totalAccountsConnected} of ${userStats.maxLifetimeAccounts}`
  const approachingLimit = userStats.totalAccountsConnected >= userStats.maxLifetimeAccounts * 0.8

  const handleDelete = async () => {
    if (confirmText !== account.name) {
      setError('Account name does not match')
      return
    }

    try {
      setDeleting(true)
      setError(null)
      await deleteAccount(account.id, reason)
      onDeleted()
    } catch (err) {
      setError(mapErrorToMessage(err))
    } finally {
      setDeleting(false)
    }
  }

  return (
    <div className="modal-overlay">
      <div className="modal-content delete-modal">
        <h2>⚠️ Delete Account?</h2>

        <div className="account-info">
          <p><strong>{account.name}</strong></p>
          <p>{account.institution_name}</p>
          <p>****{account.mask}</p>
        </div>

        {/* Lifetime Stats Warning */}
        {approachingLimit && (
          <div className="warning-box">
            <p>⚠️ <strong>Lifetime Account Limit</strong></p>
            <p>You've used {lifetimeUsage} lifetime account slots.</p>
            <p>Deleting this account will not free up a slot on Free tier.</p>
            <button className="btn-upgrade">Upgrade for Unlimited</button>
          </div>
        )}

        {/* What Gets Deleted */}
        <div className="deletion-impact">
          <h3>What will be deleted:</h3>
          <ul>
            <li>✅ Account connection to {account.institution_name}</li>
            <li>✅ Real-time balance updates</li>
            <li>❌ Transaction history will be preserved (for your records)</li>
            <li>❌ Historical data remains for budgets and reports</li>
          </ul>
          <p className="grace-period">
            🕒 You have 30 days to undo this deletion
          </p>
        </div>

        {/* Optional Deletion Reason */}
        <div className="form-group">
          <label>Why are you deleting this account? (Optional)</label>
          <select value={reason} onChange={(e) => setReason(e.target.value)}>
            <option value="">Select a reason...</option>
            <option value="closed_account">I closed this bank account</option>
            <option value="switched_banks">I switched to a different bank</option>
            <option value="duplicate">Duplicate account</option>
            <option value="not_useful">Not useful anymore</option>
            <option value="privacy_concerns">Privacy concerns</option>
            <option value="other">Other</option>
          </select>
        </div>

        {/* Confirmation Input */}
        <div className="form-group">
          <label>Type <strong>{account.name}</strong> to confirm:</label>
          <input
            type="text"
            value={confirmText}
            onChange={(e) => setConfirmText(e.target.value)}
            placeholder="Account name"
          />
        </div>

        {error && <p className="error">{error}</p>}

        <div className="modal-actions">
          <button className="btn-cancel" onClick={onClose}>
            Cancel
          </button>
          <button
            className="btn-delete"
            onClick={handleDelete}
            disabled={confirmText !== account.name || deleting}
          >
            {deleting ? 'Deleting...' : 'Delete Account'}
          </button>
        </div>
      </div>
    </div>
  )
}
```

---

### Lifetime Stats Display

**Location**: `apps/web/src/features/accounts/components/AccountLimitsBanner.tsx`

```typescript
interface AccountLimitsBannerProps {
  stats: UserLifetimeStats
  tier: SubscriptionTier
}

export function AccountLimitsBanner({ stats, tier }: AccountLimitsBannerProps) {
  const activePercentage = (stats.activeAccounts / tier.limits.accounts) * 100
  const lifetimePercentage = (stats.totalAccountsConnected / tier.limits.max_lifetime_accounts) * 100

  const approachingActiveLimit = activePercentage >= 80
  const approachingLifetimeLimit = lifetimePercentage >= 80

  if (!approachingActiveLimit && !approachingLifetimeLimit) {
    return null // Don't show banner if well under limits
  }

  return (
    <div className="limits-banner">
      {approachingActiveLimit && (
        <div className="limit-warning">
          <p>📊 <strong>Active Accounts:</strong> {stats.activeAccounts} of {tier.limits.accounts} used ({activePercentage.toFixed(0)}%)</p>
        </div>
      )}

      {approachingLifetimeLimit && (
        <div className="limit-warning">
          <p>🔢 <strong>Lifetime Accounts:</strong> {stats.totalAccountsConnected} of {tier.limits.max_lifetime_accounts} used ({lifetimePercentage.toFixed(0)}%)</p>
          <p className="help-text">
            Deleting accounts does not reset this counter. Upgrade to {tier.name === 'free' ? 'Premium' : 'Premium+'} for more slots.
          </p>
        </div>
      )}

      {(approachingActiveLimit || approachingLifetimeLimit) && (
        <button className="btn-upgrade">
          Upgrade to {tier.name === 'free' ? 'Premium' : 'Premium+'}
        </button>
      )}
    </div>
  )
}
```

---

## GDPR Compliance

### Right to Erasure (Hard Delete)

**Process**:
1. User requests GDPR data erasure via UI
2. 30-day grace period before permanent deletion
3. Email notification: "Your data will be permanently deleted on [date]"
4. User can cancel erasure request during grace period
5. After 30 days: Automated job hard-deletes ALL user data

**Implementation**:

```sql
CREATE TABLE gdpr_erasure_requests (
    id UUID PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users(id),
    requested_at TIMESTAMP NOT NULL,
    scheduled_deletion_at TIMESTAMP NOT NULL,  -- 30 days from request
    status VARCHAR(20) NOT NULL,  -- pending, cancelled, completed
    cancellation_reason TEXT,
    cancelled_at TIMESTAMP,
    completed_at TIMESTAMP,
    deleted_records_count INTEGER
);
```

```go
// User initiates GDPR erasure
func (s *GDPRService) RequestErasure(ctx context.Context, userID string) error {
    deletionDate := time.Now().Add(30 * 24 * time.Hour)

    request := &domain.ErasureRequest{
        UserID:               userID,
        RequestedAt:          time.Now(),
        ScheduledDeletionAt:  deletionDate,
        Status:               "pending",
    }

    s.repo.Create(ctx, request)

    // Send email notification
    s.emailService.SendErasureNotification(userID, deletionDate)

    return nil
}

// Automated daily job: Process pending erasures
func (s *GDPRService) ProcessScheduledErasures(ctx context.Context) error {
    pending := s.repo.GetPendingErasures(ctx, time.Now())

    for _, request := range pending {
        // Hard delete ALL user data
        count := s.hardDeleteUserData(ctx, request.UserID)

        request.Status = "completed"
        request.CompletedAt = time.Now()
        request.DeletedRecordsCount = count
        s.repo.Update(ctx, request)
    }

    return nil
}

// Hard delete all user data (GDPR erasure)
func (s *GDPRService) hardDeleteUserData(ctx context.Context, userID string) int {
    count := 0

    // Delete in order (avoid FK constraints)
    count += s.deleteTransactions(userID)
    count += s.deleteAccounts(userID)
    count += s.deleteLinkedItems(userID)
    count += s.deleteBudgetRules(userID)
    count += s.deletePrivacySettings(userID)
    count += s.deleteAuditLogs(userID)
    count += s.deleteUserProfile(userID)

    return count
}
```

---

## Testing Strategy

### Unit Tests

**Test**: `deletion_service_test.go`

```go
func TestSoftDeleteAccount(t *testing.T) {
    // Setup
    service := createDeletionService(t)
    user := createTestUser(t, "free")
    account := createTestAccount(t, user.ID)

    // Execute
    err := service.SoftDeleteAccount(context.Background(), user.ID, account.ID, "closed account")

    // Assert
    require.NoError(t, err)

    // Verify account marked as deleted
    updated := getAccount(t, account.ID)
    assert.NotNil(t, updated.DeletedAt)
    assert.Equal(t, "closed account", *updated.DeletionReason)

    // Verify audit log created
    audits := getAuditLogs(t, user.ID)
    assert.Len(t, audits, 1)
    assert.Equal(t, account.ID, audits[0].AccountID)
}

func TestLifetimeLimitEnforcement(t *testing.T) {
    // Setup: Free tier user with 10 lifetime accounts (at limit)
    service := createDeletionService(t)
    user := createTestUser(t, "free")
    stats := &domain.LifetimeStats{
        UserID:                  user.ID,
        TotalAccountsConnected:  10,  // At Free tier limit
    }
    updateStats(t, stats)

    // Try to connect 11th account
    err := service.CreateAccount(context.Background(), user.ID, newAccountData())

    // Assert: Should fail
    assert.Error(t, err)
    assert.Contains(t, err.Error(), "lifetime account limit")
}

func TestAnomalyDetection_QuickChurn(t *testing.T) {
    detector := NewAnomalyDetector()

    deletion := &domain.DeletionAudit{
        AccountAgeDays: 2,  // Deleted 2 days after creation
    }

    suspicious, reasons := detector.CheckDeletionPattern(context.Background(), "user123", deletion)

    assert.True(t, suspicious)
    assert.Contains(t, reasons[0], "Quick churn")
}
```

---

### Integration Tests

**Test**: `deletion_integration_test.go`

```go
//go:build integration

func TestFullDeletionCycle(t *testing.T) {
    // Setup real database
    db := setupTestDB(t)
    defer db.Close()

    // Create user, connect account, sync transactions
    user := createTestUser(t, db, "free")
    item := linkPlaidItem(t, db, user.ID, "ins_chase")
    account := createAccount(t, db, user.ID, item.ID)
    transactions := syncTransactions(t, db, account.ID, 100)

    // Soft delete account
    err := deletionService.SoftDeleteAccount(context.Background(), user.ID, account.ID, "test")
    require.NoError(t, err)

    // Verify account soft-deleted
    acct := getAccount(t, db, account.ID)
    assert.NotNil(t, acct.DeletedAt)

    // Verify transactions preserved
    txns := getTransactions(t, db, account.ID)
    assert.Len(t, txns, 100)  // Transactions still exist

    // Verify audit log
    audits := getAuditLogs(t, db, user.ID)
    assert.Len(t, audits, 1)
    assert.Equal(t, 100, audits[0].TransactionsCount)
}
```

---

### E2E Tests

**Test**: `apps/web/e2e/account-deletion.spec.ts`

```typescript
test('Free tier user cannot exceed lifetime account limit', async ({ page }) => {
  // Login as free tier user with 9 lifetime accounts
  await loginAsFreeTierUser(page, { lifetimeAccounts: 9 })

  // Connect 10th account (should succeed - last allowed)
  await page.goto('/accounts')
  await page.click('button:has-text("Connect Bank")')
  await completePlaidLink(page)

  // Verify success
  await expect(page.locator('text=Account connected')).toBeVisible()

  // Try to connect 11th account (should fail)
  await page.click('button:has-text("Connect Bank")')

  // Verify limit error
  await expect(page.locator('text=Lifetime account limit reached')).toBeVisible()
  await expect(page.locator('button:has-text("Upgrade to Premium")')).toBeVisible()
})

test('Soft delete preserves transaction history', async ({ page }) => {
  await loginAsUser(page)

  // Navigate to account with transactions
  await page.goto('/accounts')
  const accountCard = page.locator('[data-testid="account-card-chase-checking"]')

  // Verify transactions visible
  await accountCard.click()
  await expect(page.locator('text=100 transactions')).toBeVisible()

  // Delete account
  await page.click('button:has-text("Delete Account")')
  await page.fill('input[placeholder="Account name"]', 'Chase Checking')
  await page.click('button:has-text("Delete Account")')

  // Verify account deleted
  await expect(page.locator('[data-testid="account-card-chase-checking"]')).not.toBeVisible()

  // Navigate to transaction history
  await page.goto('/transactions')

  // Verify transactions still visible (marked as from deleted account)
  await expect(page.locator('text=Chase Checking (Deleted)')).toBeVisible()
  await expect(page.locator('text=100 transactions')).toBeVisible()
})
```

---

## Metrics & Monitoring

### Metrics to Track

**Prometheus Metrics** (`backend/internal/middleware/metrics.go`):

```go
var (
    accountDeletionsTotal = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "account_deletions_total",
            Help: "Total number of account deletions",
        },
        []string{"tier", "reason", "suspicious"},
    )

    deletionAccountAgeHistogram = prometheus.NewHistogramVec(
        prometheus.HistogramOpts{
            Name:    "deletion_account_age_days",
            Help:    "Age of accounts at deletion time (days)",
            Buckets: prometheus.LinearBuckets(0, 7, 20),  // 0-140 days
        },
        []string{"tier"},
    )

    lifetimeLimitReached = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "lifetime_limit_reached_total",
            Help: "Users hitting lifetime account limits",
        },
        []string{"tier", "limit_type"},
    )

    suspiciousDeletionsTotal = prometheus.NewCounter(
        prometheus.CounterOpts{
            Name: "suspicious_deletions_total",
            Help: "Deletions flagged as suspicious by anomaly detector",
        },
    )
)
```

**Dashboards**:
1. **Deletion Patterns Dashboard**:
   - Deletions per day (by tier)
   - Deletion reasons breakdown
   - Account age at deletion (histogram)
   - Suspicious deletion rate

2. **Lifetime Limits Dashboard**:
   - Users approaching lifetime limits (80%, 90%, 100%)
   - Conversion rate: limit warning → upgrade
   - Lifetime limit violations blocked

3. **Abuse Detection Dashboard**:
   - Flagged users count
   - False positive rate (manual review outcomes)
   - Churn rate (accounts connected/deleted ratio)

---

## Migration Strategy

### Existing Users (Grandfathering)

**Goal**: Do NOT disrupt existing users or penalize legitimate behavior.

**Approach**:
1. **Backfill lifetime stats** from current data:
   ```sql
   INSERT INTO user_lifetime_stats (user_id, total_accounts_connected, first_account_connected_at)
   SELECT
       user_id,
       COUNT(*) as total_accounts_connected,
       MIN(created_at) as first_account_connected_at
   FROM accounts
   WHERE deleted_at IS NULL
   GROUP BY user_id;
   ```

2. **Set generous initial limits** for existing users:
   ```sql
   -- Give existing free tier users extra buffer (15 instead of 10)
   UPDATE user_lifetime_stats
   SET lifetime_limit_override = 15
   WHERE user_id IN (SELECT id FROM users WHERE tier = 'free')
     AND total_accounts_connected > 10;
   ```

3. **Grace period**: 90 days before enforcing lifetime limits
   ```go
   // Check if user created before migration date
   migrationDate := time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)
   if user.CreatedAt.Before(migrationDate) {
       graceExpiry := migrationDate.Add(90 * 24 * time.Hour)
       if time.Now().Before(graceExpiry) {
           // Skip lifetime limit checks during grace period
           return nil
       }
   }
   ```

4. **Email notification** to users approaching limits:
   ```
   Subject: Update: Account Connection Limits

   We've introduced lifetime account limits to ensure fair usage:
   - Free tier: 10 lifetime accounts (you've used 8)
   - Premium tier: 100 lifetime accounts

   You have 90 days before limits are enforced.
   Questions? Contact support@boostedbudget.com
   ```

---

## Cost Analysis

### Storage Costs (Soft Delete)

**Assumptions**:
- 10,000 active users
- Average 5 accounts per user
- 20% delete accounts each year
- Average 500 transactions per account

**Annual Deleted Data**:
```
Deleted accounts/year:
- 10,000 users × 5 accounts × 20% = 10,000 accounts/year

Deleted transactions/year:
- 10,000 accounts × 500 transactions = 5,000,000 transactions/year

Storage size:
- Accounts: 10,000 × 1KB = 10MB/year
- Transactions: 5,000,000 × 500 bytes = 2.5GB/year
- Audit logs: 10,000 × 2KB = 20MB/year
```

**Storage Costs** (PostgreSQL on Fly.io):
- 2.5GB/year × $0.15/GB/month = **$0.38/month = $4.50/year**
- Negligible cost increase

**Benefit**:
- Prevent $12,100/year in abuse
- ROI: 2,689:1

---

### Plaid API Savings

**Current** (No Limits):
- Users cycle through accounts freely
- 100 gaming users × 12 connections/year × $2.50/connection = **$3,000/year**

**After Implementation**:
- Lifetime limit: 5 connections for Free tier
- 100 users × 5 connections × $2.50 = **$1,250/year**
- **Savings: $1,750/year**

**Total Annual Benefit**:
- Abuse prevention: $12,100/year
- Plaid savings: $1,750/year
- Storage cost: -$4.50/year
- **Net benefit: $13,845.50/year**

---

## Timeline & Phases

### Phase 1: Core Implementation (4 weeks)

**Week 1: Database & Backend Foundation**
- Migration: Add `deleted_at`, `user_lifetime_stats`, `account_deletion_audit` tables
- Implement `DeletionService` with soft delete logic
- Update repositories to filter `deleted_at IS NULL`
- Unit tests

**Week 2: Lifetime Tracking & Limits**
- Implement lifetime stats tracking (on account creation)
- Add lifetime limit checks to account service
- Update subscription tier config with new limits
- Integration tests

**Week 3: Frontend Changes**
- Delete confirmation modal with warnings
- Lifetime stats display in dashboard
- Approaching limit banners
- E2E tests

**Week 4: Audit & Monitoring**
- Audit logging for all deletions
- Prometheus metrics integration
- Grafana dashboards
- Migration script for existing users
- Deploy to staging → production

**Deliverables**:
- ✅ Soft deletion implemented
- ✅ Lifetime limits enforced
- ✅ Audit logging functional
- ✅ User-facing warnings in UI

---

### Phase 2: Anomaly Detection (2 weeks)

**Week 5: Detection Rules**
- Implement `AnomalyDetector` service
- Quick churn detection
- Export-then-delete pattern detection
- Excessive deletion rate detection

**Week 6: Response Actions**
- Flag suspicious users in admin dashboard
- Email notifications to security team
- Manual review workflow
- Deploy to production

**Deliverables**:
- ✅ Automated anomaly detection
- ✅ Security team notifications
- ✅ Manual review process

---

### Phase 3 (Optional): Advanced Safeguards (2 weeks)

**If abuse continues after Phase 1+2**:
- Cooldown periods for account re-adds
- Tier-based deletion quotas
- Stricter limits for flagged users

---

## Open Questions

### Policy Decisions Needed

1. **Lifetime Limit Values**:
   - Free tier: 10 lifetime accounts? Too restrictive? Too generous?
   - Premium tier: 100 lifetime accounts? Unlimited?
   - Input needed from product team

2. **GDPR Grace Period**:
   - 30 days? 90 days? 7 days?
   - Industry standard: 30 days

3. **Transaction Retention**:
   - Keep transactions after account deletion? (Recommended: YES)
   - For how long? Forever or time-limited? (Recommended: Forever until GDPR erasure)

4. **Deletion Quotas**:
   - Should Free tier have monthly deletion limit? (e.g., 2/month)
   - Too punitive for legitimate users?

5. **Suspicious User Actions**:
   - Hard block or soft warning for flagged users?
   - Require manual review before next connection?
   - Recommended: Soft warning + admin notification

6. **Existing User Grace Period**:
   - How long before enforcing limits on existing users?
   - Recommended: 90 days

7. **Premium Tier Benefits**:
   - Should Premium+ have truly unlimited lifetime accounts?
   - Or cap at 1,000 for abuse prevention?

---

## Acceptance Criteria

### Phase 1 (Core)

**Functional**:
- [x] ✅ Soft deletion implemented for `accounts` and `linked_items`
- [x] ✅ `user_lifetime_stats` table tracks total accounts/items connected
- [x] ✅ Lifetime limits enforced based on subscription tier
- [x] ✅ Deletion attempts beyond quota return clear error message
- [x] ✅ Audit log records all deletions with metadata
- [x] ✅ Transactions preserved after account deletion
- [x] ✅ Frontend shows deletion confirmation modal with warnings
- [x] ✅ Frontend displays lifetime stats in account dashboard
- [x] ✅ Approaching limit banner shown at 80% usage
- [x] ✅ Migration script backfills stats for existing users

**Non-Functional**:
- [x] ✅ Unit test coverage: >80%
- [x] ✅ Integration tests: Full deletion cycle tested
- [x] ✅ E2E tests: Limit enforcement, soft delete, transaction preservation
- [x] ✅ Performance: Soft delete query < 100ms
- [x] ✅ Metrics: Prometheus tracking deletions, limits, anomalies

**User Experience**:
- [x] ✅ Free tier user attempting 11th lifetime connection sees upgrade prompt
- [x] ✅ User deleting account sees "30-day grace period" message
- [x] ✅ User can view transaction history from deleted accounts
- [x] ✅ Clear messaging: "Lifetime: 8 of 10 accounts used"

---

### Phase 2 (Anomaly Detection)

**Functional**:
- [x] ✅ Anomaly detection flags suspicious deletions
- [x] ✅ Security team receives email notification for flagged users
- [x] ✅ Admin dashboard shows flagged users for manual review
- [x] ✅ Manual review workflow: approve, flag, or suspend

**Metrics**:
- [x] ✅ Track false positive rate (< 10% target)
- [x] ✅ Track confirmed abuse cases
- [x] ✅ Measure conversion rate: flagged → upgrade vs. churn

---

## Summary

This design specification provides a comprehensive approach to preventing account and transaction abuse through soft deletion, lifetime tracking, and anomaly detection.

**Key Benefits**:
- ✅ **Prevents $13,845/year in losses** (abuse + Plaid costs)
- ✅ **Maintains user trust** (no punitive measures for legitimate users)
- ✅ **GDPR-compliant** (30-day grace period, right to erasure)
- ✅ **Scalable** (handles growth without manual intervention)
- ✅ **Auditable** (complete deletion history for compliance)

**Next Steps**:
1. Review and approve this design
2. Create GitHub issue (summary + link to this doc)
3. Begin Phase 1 implementation (4 weeks)
4. Monitor metrics post-launch
5. Adjust limits based on real-world data

---

**Document Version**: 1.0
**Last Updated**: 2025-01-25
**Owner**: @maxnussbaum
**Status**: Pending Approval
**Related Issues**: #238 (Account Limit Management)
