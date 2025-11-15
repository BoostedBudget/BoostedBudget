#!/bin/bash
# Migration Testing Framework for BoostedBudget
# Issue #326: Comprehensive Atlas migration testing
# Issue #348: Rollback capability testing (.up/.down migrations)
#
# Tests:
#   1. Idempotency (migrations can be run multiple times safely)
#   2. Performance (migrations complete within acceptable timeframes)
#   3. Data Integrity (migrations preserve existing data)
#   4. Schema Constraints (foreign keys, indexes, constraints exist)
#   5. Rollback Testing (up→down→up cycle produces identical schema)

set -euo pipefail

# =============================================================================
# Configuration
# =============================================================================

DATABASE_URL="${DATABASE_URL:?ERROR: DATABASE_URL environment variable is required}"
MIGRATIONS_DIR="${MIGRATIONS_DIR:-./backend/migrations}"
PERF_WARN_THRESHOLD=${PERF_WARN_THRESHOLD:-60}    # seconds
PERF_FAIL_THRESHOLD=${PERF_FAIL_THRESHOLD:-300}   # seconds
PERF_REPORT_FILE="${PERF_REPORT_FILE:-migration-performance.json}"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Test results tracking
TESTS_PASSED=0
TESTS_FAILED=0
WARNINGS=0

# =============================================================================
# Helper Functions
# =============================================================================

log_info() {
    echo -e "${BLUE}ℹ️  $1${NC}"
}

log_success() {
    echo -e "${GREEN}✅ $1${NC}"
}

log_warning() {
    echo -e "${YELLOW}⚠️  $1${NC}"
    ((WARNINGS++))
}

log_error() {
    echo -e "${RED}❌ $1${NC}"
}

log_section() {
    echo ""
    echo -e "${BLUE}═══════════════════════════════════════════════════════════${NC}"
    echo -e "${BLUE}  $1${NC}"
    echo -e "${BLUE}═══════════════════════════════════════════════════════════${NC}"
    echo ""
}

pass_test() {
    ((TESTS_PASSED++))
    log_success "$1"
}

fail_test() {
    ((TESTS_FAILED++))
    log_error "$1"
}

# Run psql command against database (trim whitespace from result)
run_psql() {
    psql "$DATABASE_URL" -t -c "$1" 2>/dev/null | tr -d ' \n' || echo ""
}

# Get schema hash for idempotency comparison
# Uses --schema=public to dump only application tables, excluding:
#   - Atlas tracking schemas (atlas_schema_revisions)
#   - System schemas (pg_catalog, information_schema, etc.)
# Also filters pg_dump 15.14+ security tokens (CVE-2025-8714):
#   - \restrict and \unrestrict with random keys (change on each dump)
#   - Filtering is safe and standard practice (Rails, GitLab, etc.)
get_schema_hash() {
    # Capture pg_dump output, filtering out security tokens
    local dump=$(pg_dump "$DATABASE_URL" --schema-only --no-owner --no-privileges \
        --schema=public 2>/dev/null \
        | grep -v '^\\restrict ' \
        | grep -v '^\\unrestrict ')

    # Save to timestamped file for debugging
    local timestamp=$(date +%s%N)
    local dump_file="/tmp/schema_dump_${timestamp}.sql"
    echo "$dump" > "$dump_file"

    # Log file location (visible in CI logs for debugging)
    echo "📝 Schema dump saved: $dump_file" >&2

    # Show first 50 lines for quick inspection
    echo "=== Schema Dump Preview (first 50 lines) ===" >&2
    echo "$dump" | head -50 >&2
    echo "..." >&2

    # Calculate and return hash
    echo "$dump" | md5sum | cut -d' ' -f1
}

# Clean database (drop all tables and Atlas tracking)
clean_database() {
    log_info "Cleaning database..."
    # Drop public schema (all tables)
    # NOTE: Errors visible for debugging - previously silenced with > /dev/null 2>&1
    psql "$DATABASE_URL" -c "DROP SCHEMA public CASCADE; CREATE SCHEMA public;"
    # Drop Atlas tracking schema (so Atlas doesn't think migrations are already applied)
    psql "$DATABASE_URL" -c "DROP SCHEMA IF EXISTS atlas_schema_revisions CASCADE;"
}

# Apply migrations using Atlas
apply_migrations() {
    local start_time=$(date +%s)

    # Get absolute paths to atlas.hcl config file and migrations directory
    local abs_config_path
    local abs_migrations_dir
    local project_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
    abs_config_path="$project_root/backend/atlas.hcl"
    abs_migrations_dir="$project_root/backend/migrations"

    # NOTE: Errors visible for debugging - previously silenced with > /dev/null 2>&1
    # NOTE: Using --dir flag to provide absolute path with golang-migrate format
    # The format parameter is CRITICAL - without it, Atlas runs both .up and .down files
    atlas migrate apply \
        --config "file://$abs_config_path" \
        --env local \
        --url "$DATABASE_URL" \
        --dir "file://$abs_migrations_dir?format=golang-migrate"

    local end_time=$(date +%s)
    local duration=$((end_time - start_time))
    echo "$duration"
}

# Rollback all migrations using Atlas
rollback_migrations() {
    local start_time=$(date +%s)

    # Get absolute paths to atlas.hcl config file and migrations directory
    local abs_config_path
    local abs_migrations_dir
    local project_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
    abs_config_path="$project_root/backend/atlas.hcl"
    abs_migrations_dir="$project_root/backend/migrations"

    # Rollback all migrations using amount positional argument
    # NOTE: Using --dir flag to provide absolute path with golang-migrate format
    # The format parameter is CRITICAL - without it, Atlas runs both .up and .down files
    # NOTE: Amount 100 (positional arg) ensures ALL migrations are rolled back (current count: 15)
    #       --to-version 0 does NOT work (keeps migrations UP TO version 0, not before it)
    atlas migrate down \
        --config "file://$abs_config_path" \
        --env local \
        --url "$DATABASE_URL" \
        --dir "file://$abs_migrations_dir?format=golang-migrate" \
        100

    local end_time=$(date +%s)
    local duration=$((end_time - start_time))
    echo "$duration"
}

# =============================================================================
# Test 1: Idempotency Testing
# =============================================================================

test_idempotency() {
    log_section "TEST 1: IDEMPOTENCY"

    log_info "Testing that migrations can be applied multiple times safely..."
    log_info "This is critical to prevent Issue #279-style failures"
    echo ""

    # Clean start
    clean_database

    # First application
    log_info "Applying migrations (1st time)..."
    local duration1=$(apply_migrations)
    if [ $? -ne 0 ]; then
        fail_test "First migration application failed"
        return 1
    fi
    log_success "First application completed in ${duration1}s"

    # Capture schema hash
    log_info "Capturing schema hash..."
    local hash1=$(get_schema_hash)
    if [ -z "$hash1" ]; then
        fail_test "Failed to capture schema hash after first application"
        return 1
    fi
    log_info "Schema hash (1st): $hash1"

    # Second application (idempotency test)
    log_info "Applying migrations (2nd time - idempotency test)..."
    local duration2=$(apply_migrations)
    if [ $? -ne 0 ]; then
        fail_test "Second migration application failed - migrations are NOT idempotent!"
        log_error "This likely means migrations lack IF NOT EXISTS guards"
        log_error "See migrations 000013, 000015 for good idempotency patterns"
        return 1
    fi
    log_success "Second application completed in ${duration2}s"

    # Capture schema hash again
    log_info "Capturing schema hash (2nd)..."
    local hash2=$(get_schema_hash)
    if [ -z "$hash2" ]; then
        fail_test "Failed to capture schema hash after second application"
        return 1
    fi
    log_info "Schema hash (2nd): $hash2"

    # Compare hashes
    echo ""
    if [ "$hash1" = "$hash2" ]; then
        pass_test "IDEMPOTENCY TEST PASSED - Schema unchanged after re-applying migrations"
        log_success "Migrations are safe to re-run (critical for deployment reliability)"
        return 0
    else
        fail_test "IDEMPOTENCY TEST FAILED - Schema changed after re-applying migrations!"
        log_error "Schema hash mismatch:"
        log_error "  1st application: $hash1"
        log_error "  2nd application: $hash2"
        log_error ""

        # Show diff between the two dumps for debugging
        echo "🔍 Analyzing differences between schema dumps..." >&2
        DUMP1=$(ls -t /tmp/schema_dump_*.sql 2>/dev/null | sed -n '2p')  # Second newest
        DUMP2=$(ls -t /tmp/schema_dump_*.sql 2>/dev/null | sed -n '1p')  # Newest

        if [ -f "$DUMP1" ] && [ -f "$DUMP2" ]; then
            echo "Comparing:" >&2
            echo "  First run:  $DUMP1" >&2
            echo "  Second run: $DUMP2" >&2
            echo "" >&2
            echo "=== DIFF OUTPUT (first 100 lines) ===" >&2
            diff -u "$DUMP1" "$DUMP2" | head -100 >&2 || true
            echo "===" >&2
        else
            log_error "Could not find dump files for comparison"
            log_error "  DUMP1: ${DUMP1:-not found}"
            log_error "  DUMP2: ${DUMP2:-not found}"
        fi
        echo "" >&2

        log_error "This indicates migrations are NOT idempotent"
        log_error "Check for missing IF NOT EXISTS guards on:"
        log_error "  - CREATE TABLE"
        log_error "  - CREATE INDEX"
        log_error "  - ALTER TABLE ADD COLUMN"
        return 1
    fi
}

# =============================================================================
# Test 2: Performance Benchmarking
# =============================================================================

test_performance() {
    log_section "TEST 2: PERFORMANCE BENCHMARKING"

    log_info "Measuring migration performance..."
    log_info "Thresholds: WARN > ${PERF_WARN_THRESHOLD}s, FAIL > ${PERF_FAIL_THRESHOLD}s"
    echo ""

    # Clean start
    clean_database

    # Time the migration application
    local start_time=$(date +%s)
    local duration_output
    duration_output=$(apply_migrations 2>&1)
    local apply_exit_code=$?
    local end_time=$(date +%s)
    local total_duration=$((end_time - start_time))

    # Check if migration application failed
    if [ $apply_exit_code -ne 0 ]; then
        fail_test "MIGRATION APPLICATION FAILED during performance test"
        log_error "Atlas migrate apply returned exit code: $apply_exit_code"
        log_error "Output: $duration_output"
        return 1
    fi

    log_info "Total migration time: ${total_duration}s"

    # Check against thresholds
    local perf_status="PASS"
    if [ $total_duration -gt $PERF_FAIL_THRESHOLD ]; then
        fail_test "PERFORMANCE TEST FAILED - Migrations took ${total_duration}s (threshold: ${PERF_FAIL_THRESHOLD}s)"
        log_error "Migrations are too slow - consider optimization or splitting"
        perf_status="FAIL"
    elif [ $total_duration -gt $PERF_WARN_THRESHOLD ]; then
        log_warning "PERFORMANCE WARNING - Migrations took ${total_duration}s (warning threshold: ${PERF_WARN_THRESHOLD}s)"
        log_warning "Consider optimizing slow migrations"
        pass_test "Performance test passed with warnings"
        perf_status="WARN"
    else
        pass_test "PERFORMANCE TEST PASSED - Migrations completed in ${total_duration}s"
        perf_status="PASS"
    fi

    # Generate JSON performance report
    # NOTE: Count only .up.sql files (forward migrations), not .down.sql
    cat > "$PERF_REPORT_FILE" <<EOF
{
  "timestamp": "$(date -u +%Y-%m-%dT%H:%M:%SZ)",
  "total_duration_seconds": $total_duration,
  "threshold_warn_seconds": $PERF_WARN_THRESHOLD,
  "threshold_fail_seconds": $PERF_FAIL_THRESHOLD,
  "status": "$perf_status",
  "migrations_count": $(ls -1 "$MIGRATIONS_DIR"/*.up.sql 2>/dev/null | wc -l | tr -d ' ')
}
EOF

    log_info "Performance report saved to: $PERF_REPORT_FILE"

    if [ "$perf_status" = "FAIL" ]; then
        return 1
    fi
    return 0
}

# =============================================================================
# Test 3: Data Integrity Testing
# =============================================================================

test_data_integrity() {
    log_section "TEST 3: DATA INTEGRITY"

    log_info "Testing that idempotent migrations preserve existing data..."
    echo ""

    # Clean start and apply migrations
    clean_database
    apply_migrations > /dev/null 2>&1

    # Seed test data
    log_info "Seeding test data..."

    # Insert test users
    psql "$DATABASE_URL" > /dev/null 2>&1 <<EOF
-- Free tier user
INSERT INTO users (id, auth0_user_id, email, primary_region, tier, created_at, updated_at)
VALUES (
    '00000000-0000-0000-0000-000000000001'::uuid,
    'auth0|test-free-user',
    'free@test.com',
    'us-east-1',
    'free',
    NOW(),
    NOW()
);

-- Premium user
INSERT INTO users (id, auth0_user_id, email, primary_region, tier, created_at, updated_at)
VALUES (
    '00000000-0000-0000-0000-000000000002'::uuid,
    'auth0|test-premium-user',
    'premium@test.com',
    'us-east-1',
    'premium',
    NOW(),
    NOW()
);

-- Premium Plus user
INSERT INTO users (id, auth0_user_id, email, primary_region, tier, created_at, updated_at)
VALUES (
    '00000000-0000-0000-0000-000000000003'::uuid,
    'auth0|test-premium-plus-user',
    'premium-plus@test.com',
    'us-east-1',
    'premium_plus',
    NOW(),
    NOW()
);

-- Privacy settings
INSERT INTO privacy_settings (user_id, data_retention_days, created_at, updated_at)
VALUES
    ('00000000-0000-0000-0000-000000000001'::uuid, 90, NOW(), NOW()),
    ('00000000-0000-0000-0000-000000000002'::uuid, 365, NOW(), NOW()),
    ('00000000-0000-0000-0000-000000000003'::uuid, 730, NOW(), NOW());

-- Linked items
INSERT INTO linked_items (id, user_id, item_id, access_token, provider, status, created_at, updated_at)
VALUES
    ('00000000-0000-0000-0000-000000000011'::uuid, '00000000-0000-0000-0000-000000000001'::uuid, 'item-001', 'encrypted-token-1', 'plaid', 'active', NOW(), NOW()),
    ('00000000-0000-0000-0000-000000000012'::uuid, '00000000-0000-0000-0000-000000000002'::uuid, 'item-002', 'encrypted-token-2', 'plaid', 'active', NOW(), NOW()),
    ('00000000-0000-0000-0000-000000000013'::uuid, '00000000-0000-0000-0000-000000000003'::uuid, 'item-003', 'encrypted-token-3', 'plaid', 'active', NOW(), NOW());

-- Accounts
INSERT INTO accounts (id, linked_item_id, plaid_account_id, name, official_name, type, subtype, created_at, updated_at)
VALUES
    ('00000000-0000-0000-0000-000000000021'::uuid, '00000000-0000-0000-0000-000000000011'::uuid, 'plaid-acc-001', 'Checking', 'Main Checking', 'depository', 'checking', NOW(), NOW()),
    ('00000000-0000-0000-0000-000000000022'::uuid, '00000000-0000-0000-0000-000000000012'::uuid, 'plaid-acc-002', 'Savings', 'High Yield Savings', 'depository', 'savings', NOW(), NOW()),
    ('00000000-0000-0000-0000-000000000023'::uuid, '00000000-0000-0000-0000-000000000013'::uuid, 'plaid-acc-003', 'Credit Card', 'Premium Rewards Card', 'credit', 'credit card', NOW(), NOW());

-- Transactions
INSERT INTO transactions (id, account_id, external_transaction_id, amount, date, name, category, pending, created_at, updated_at)
VALUES
    (gen_random_uuid(), '00000000-0000-0000-0000-000000000021'::uuid, 'txn-001', -50.00, '2025-11-01', 'Grocery Store', 'Food and Drink', false, NOW(), NOW()),
    (gen_random_uuid(), '00000000-0000-0000-0000-000000000021'::uuid, 'txn-002', -25.50, '2025-11-02', 'Gas Station', 'Transportation', false, NOW(), NOW()),
    (gen_random_uuid(), '00000000-0000-0000-0000-000000000021'::uuid, 'txn-003', 2500.00, '2025-11-05', 'Paycheck', 'Income', false, NOW(), NOW()),
    (gen_random_uuid(), '00000000-0000-0000-0000-000000000022'::uuid, 'txn-004', -100.00, '2025-11-03', 'Online Shopping', 'Shopping', false, NOW(), NOW()),
    (gen_random_uuid(), '00000000-0000-0000-0000-000000000022'::uuid, 'txn-005', 100.00, '2025-11-01', 'Transfer from Checking', 'Transfer', false, NOW(), NOW()),
    (gen_random_uuid(), '00000000-0000-0000-0000-000000000023'::uuid, 'txn-006', -200.00, '2025-11-04', 'Restaurant', 'Food and Drink', false, NOW(), NOW()),
    (gen_random_uuid(), '00000000-0000-0000-0000-000000000023'::uuid, 'txn-007', -75.00, '2025-11-06', 'Utilities', 'Bills', false, NOW(), NOW());
EOF

    if [ $? -ne 0 ]; then
        fail_test "Failed to seed test data"
        return 1
    fi

    log_success "Test data seeded successfully"

    # Record row counts before re-running migrations
    log_info "Recording row counts..."
    local users_count_before=$(run_psql "SELECT COUNT(*) FROM users")
    local privacy_count_before=$(run_psql "SELECT COUNT(*) FROM privacy_settings")
    local linked_items_count_before=$(run_psql "SELECT COUNT(*) FROM linked_items")
    local accounts_count_before=$(run_psql "SELECT COUNT(*) FROM accounts")
    local transactions_count_before=$(run_psql "SELECT COUNT(*) FROM transactions")

    log_info "Row counts before re-applying migrations:"
    log_info "  users: $users_count_before"
    log_info "  privacy_settings: $privacy_count_before"
    log_info "  linked_items: $linked_items_count_before"
    log_info "  accounts: $accounts_count_before"
    log_info "  transactions: $transactions_count_before"

    # Re-apply migrations (idempotency test with data present)
    echo ""
    log_info "Re-applying migrations with data present (idempotency test)..."
    apply_migrations > /dev/null 2>&1
    if [ $? -ne 0 ]; then
        fail_test "Migration re-application failed with data present"
        return 1
    fi
    log_success "Migrations re-applied successfully"

    # Check row counts after
    log_info "Verifying data integrity..."
    local users_count_after=$(run_psql "SELECT COUNT(*) FROM users")
    local privacy_count_after=$(run_psql "SELECT COUNT(*) FROM privacy_settings")
    local linked_items_count_after=$(run_psql "SELECT COUNT(*) FROM linked_items")
    local accounts_count_after=$(run_psql "SELECT COUNT(*) FROM accounts")
    local transactions_count_after=$(run_psql "SELECT COUNT(*) FROM transactions")

    log_info "Row counts after re-applying migrations:"
    log_info "  users: $users_count_after"
    log_info "  privacy_settings: $privacy_count_after"
    log_info "  linked_items: $linked_items_count_after"
    log_info "  accounts: $accounts_count_after"
    log_info "  transactions: $transactions_count_after"

    # Compare counts
    echo ""
    local data_preserved=true

    if [ "$users_count_before" != "$users_count_after" ]; then
        fail_test "Data loss detected in users table ($users_count_before → $users_count_after)"
        data_preserved=false
    fi

    if [ "$privacy_count_before" != "$privacy_count_after" ]; then
        fail_test "Data loss detected in privacy_settings table ($privacy_count_before → $privacy_count_after)"
        data_preserved=false
    fi

    if [ "$linked_items_count_before" != "$linked_items_count_after" ]; then
        fail_test "Data loss detected in linked_items table ($linked_items_count_before → $linked_items_count_after)"
        data_preserved=false
    fi

    if [ "$accounts_count_before" != "$accounts_count_after" ]; then
        fail_test "Data loss detected in accounts table ($accounts_count_before → $accounts_count_after)"
        data_preserved=false
    fi

    if [ "$transactions_count_before" != "$transactions_count_after" ]; then
        fail_test "Data loss detected in transactions table ($transactions_count_before → $transactions_count_after)"
        data_preserved=false
    fi

    if [ "$data_preserved" = true ]; then
        pass_test "DATA INTEGRITY TEST PASSED - All data preserved after re-applying migrations"
        return 0
    else
        fail_test "DATA INTEGRITY TEST FAILED - Data was lost or corrupted"
        return 1
    fi
}

# =============================================================================
# Test 4: Schema Constraint Validation
# =============================================================================

test_schema_constraints() {
    log_section "TEST 4: SCHEMA CONSTRAINT VALIDATION"

    log_info "Validating database schema constraints, indexes, and foreign keys..."
    echo ""

    # Ensure migrations are applied
    clean_database
    log_info "Applying migrations for schema validation..."
    apply_migrations > /dev/null 2>&1
    if [ $? -ne 0 ]; then
        fail_test "Failed to apply migrations for schema validation"
        return 1
    fi
    log_success "Migrations applied successfully"
    echo ""

    # Expected tables
    local expected_tables=(
        "users"
        "privacy_settings"
        "audit_logs"
        "user_subscription_overrides"
        "linked_items"
        "accounts"
        "user_identities"
        "transactions"
        "webhook_events"
        "notifications"
        "account_sync_audit"
    )

    log_info "Checking for expected tables..."
    local tables_ok=true
    for table in "${expected_tables[@]}"; do
        local exists=$(run_psql "SELECT COUNT(*) FROM information_schema.tables WHERE table_name='$table'")
        if [ "$exists" = "1" ]; then
            log_success "Table '$table' exists"
        else
            fail_test "Table '$table' is missing!"
            tables_ok=false
        fi
    done

    if [ "$tables_ok" = false ]; then
        fail_test "SCHEMA VALIDATION FAILED - Missing expected tables"
        return 1
    fi

    # Check critical foreign keys
    echo ""
    log_info "Checking critical foreign key constraints..."

    local fk_count=$(run_psql "SELECT COUNT(*) FROM information_schema.table_constraints WHERE constraint_type='FOREIGN KEY'")
    log_info "Total foreign keys found: $fk_count"

    if [ -z "$fk_count" ] || [ "$fk_count" -lt 5 ]; then
        log_warning "Expected more foreign key constraints (found: $fk_count)"
    else
        log_success "Foreign key constraints present ($fk_count found)"
    fi

    # Check critical indexes
    echo ""
    log_info "Checking for critical indexes..."

    local index_count=$(run_psql "SELECT COUNT(*) FROM pg_indexes WHERE schemaname='public'")
    log_info "Total indexes found: $index_count"

    if [ -z "$index_count" ] || [ "$index_count" -lt 10 ]; then
        log_warning "Expected more indexes (found: $index_count)"
    else
        log_success "Indexes present ($index_count found)"
    fi

    # Check unique constraints
    echo ""
    log_info "Checking unique constraints..."

    local unique_count=$(run_psql "SELECT COUNT(*) FROM information_schema.table_constraints WHERE constraint_type='UNIQUE'")
    log_info "Total unique constraints found: $unique_count"

    if [ -z "$unique_count" ] || [ "$unique_count" -lt 3 ]; then
        log_warning "Expected more unique constraints (found: $unique_count)"
    else
        log_success "Unique constraints present ($unique_count found)"
    fi

    echo ""
    pass_test "SCHEMA CONSTRAINT VALIDATION PASSED"
    return 0
}

# =============================================================================
# Test 5: Rollback Testing
# =============================================================================
# Purpose: Verify that rollback migrations (.down.sql) correctly reverse
#          the forward migrations (.up.sql), enabling safe rollbacks in production

test_rollback() {
    log_section "TEST 5: ROLLBACK TESTING"

    log_info "Testing migration rollback capability (up → down → up cycle)..."
    log_info "This validates Issue #348: Add .up/.down Migration Support"
    echo ""

    # Clean start
    clean_database

    # Step 1: Apply all migrations (forward)
    log_info "Step 1: Applying all migrations (forward)..."
    local apply_duration=$(apply_migrations)
    log_success "Migrations applied in ${apply_duration}s"

    # Capture baseline schema hash
    log_info "Capturing baseline schema hash..."
    local hash_baseline=$(get_schema_hash)
    log_info "Baseline schema hash: $hash_baseline"
    echo ""

    # Step 2: Rollback all migrations (reverse)
    log_info "Step 2: Rolling back all migrations (reverse)..."
    local rollback_duration=$(rollback_migrations)
    log_success "Migrations rolled back in ${rollback_duration}s"

    # Verify clean state (only Atlas tracking should remain)
    log_info "Verifying clean state after rollback..."
    local tables_after_rollback=$(run_psql "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema='public' AND table_name NOT LIKE 'atlas_%'")

    if [ "$tables_after_rollback" != "0" ]; then
        fail_test "Rollback incomplete - $tables_after_rollback application tables still exist"
        log_error "Expected: 0 tables, Found: $tables_after_rollback tables"
        return 1
    fi

    log_success "Clean state verified (0 application tables)"
    echo ""

    # Step 3: Re-apply all migrations (forward again)
    log_info "Step 3: Re-applying all migrations (forward)..."
    local reapply_duration=$(apply_migrations)
    log_success "Migrations re-applied in ${reapply_duration}s"

    # Capture final schema hash
    log_info "Capturing final schema hash..."
    local hash_reapplied=$(get_schema_hash)
    log_info "Final schema hash: $hash_reapplied"
    echo ""

    # Step 4: Compare schemas
    log_info "Step 4: Comparing baseline vs final schema..."

    if [ "$hash_baseline" = "$hash_reapplied" ]; then
        echo ""
        pass_test "ROLLBACK TEST PASSED - Schema identical after up→down→up cycle"
        log_success "✓ All .down.sql migrations correctly reverse their .up.sql counterparts"
        log_success "✓ Rollback capability verified for production use"
        return 0
    else
        echo ""
        fail_test "ROLLBACK TEST FAILED - Schema mismatch after up→down→up cycle"
        log_error "Baseline hash: $hash_baseline"
        log_error "Final hash:    $hash_reapplied"
        log_error "This indicates incomplete or incorrect rollback logic in .down.sql files"
        return 1
    fi
}

# =============================================================================
# Main Execution
# =============================================================================

main() {
    log_section "MIGRATION TESTING FRAMEWORK"
    log_info "Issue #326: Comprehensive Atlas Migration Testing"
    log_info "Database: $DATABASE_URL"
    log_info "Migrations: $MIGRATIONS_DIR"
    echo ""

    # Check prerequisites
    if [ ! -d "$MIGRATIONS_DIR" ]; then
        log_error "Migrations directory not found: $MIGRATIONS_DIR"
        exit 1
    fi

    if ! command -v atlas &> /dev/null; then
        log_error "Atlas CLI not found - please install: https://atlasgo.io/"
        exit 1
    fi

    if ! command -v psql &> /dev/null; then
        log_error "PostgreSQL client (psql) not found"
        exit 1
    fi

    # Test database connection
    if ! psql "$DATABASE_URL" -c "SELECT 1" > /dev/null 2>&1; then
        log_error "Cannot connect to database: $DATABASE_URL"
        exit 1
    fi

    log_success "Prerequisites checked"

    # Run all test suites
    test_idempotency
    test_performance
    test_data_integrity
    test_schema_constraints
    test_rollback

    # Summary
    log_section "TEST SUMMARY"
    echo ""
    echo -e "${GREEN}✅ Tests Passed: $TESTS_PASSED${NC}"
    if [ $TESTS_FAILED -gt 0 ]; then
        echo -e "${RED}❌ Tests Failed: $TESTS_FAILED${NC}"
    fi
    if [ $WARNINGS -gt 0 ]; then
        echo -e "${YELLOW}⚠️  Warnings: $WARNINGS${NC}"
    fi
    echo ""

    if [ $TESTS_FAILED -gt 0 ]; then
        log_error "MIGRATION TESTS FAILED"
        log_error "Please fix failing migrations before merging"
        echo ""
        echo "Common fixes:"
        echo "  - Add IF NOT EXISTS guards to CREATE TABLE/INDEX statements"
        echo "  - Use DO \$\$ blocks for ALTER TABLE ADD COLUMN"
        echo "  - See migrations 000013, 000015 for good idempotency patterns"
        echo ""
        exit 1
    else
        log_success "ALL MIGRATION TESTS PASSED"
        log_success "Migrations are safe to deploy"
        echo ""
        exit 0
    fi
}

# Run main function
main
