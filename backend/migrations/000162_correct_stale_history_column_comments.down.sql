-- Rollback for 000162 (#1569 T6 fix round 2). Restores 000160's original comment text verbatim,
-- including the MAX(captured_at) rule that is wrong — a `down` must reproduce the prior state, not
-- an improved one. The rule is wrong in the code either way; only the documentation moves here.

COMMENT ON COLUMN holding_snapshots.captured_at IS
    'Transaction timestamp of the capture that last wrote this row. All rows written by one '
    'capture of one account share it, so MAX(captured_at) per (account_id, as_of_date) identifies '
    'that day''s final position set and excludes securities sold earlier the same day.';

COMMENT ON COLUMN liability_statements.captured_at IS
    'Transaction timestamp of the capture that last wrote this row. See holding_snapshots.captured_at.';

COMMENT ON TABLE holding_snapshots IS
    'Append-only history of investment positions (#1569 T6). Written in the SAME transaction as '
    'holdings'' full-replace, in addition to it — the holdings table keeps its replace semantics '
    'unchanged. One row per (account_id, security_id, as_of_date) UTC day; a later capture on the '
    'same day overwrites that day''s row and refreshes captured_at. Nothing here is ever deleted '
    'by a sync, so a disconnected institution''s positions stay queryable.';
