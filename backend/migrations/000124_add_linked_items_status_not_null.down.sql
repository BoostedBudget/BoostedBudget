-- Down migration: revert linked_items.status to nullable. Safe and idempotent — DROP NOT NULL
-- does not error even if the column is already nullable.
ALTER TABLE linked_items ALTER COLUMN status DROP NOT NULL;
