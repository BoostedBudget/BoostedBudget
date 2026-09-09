-- Reverses 000190. Dropping the column discards which rows belonged to a closed contributions
-- period, so a goal that round-tripped after this migration will resume double-counting on the
-- next recompute -- that is the pre-#1625 behaviour this reverts to, not new damage.
ALTER TABLE savings_goal_contributions
    DROP COLUMN IF EXISTS archived_at;
