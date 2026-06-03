-- Issue #1203: partial index for the "unreviewed inbox" query (reviewed = false).
-- Created CONCURRENTLY as the sole statement in this file (mirrors 000077).
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_transactions_user_unreviewed
    ON transactions (user_id, date DESC) WHERE reviewed = false;
