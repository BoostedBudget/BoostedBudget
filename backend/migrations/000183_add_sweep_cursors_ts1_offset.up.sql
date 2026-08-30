-- #1569 round 15 task 9 fix 1 — sweep_cursors gains a COUNT so a resume position can point INSIDE a
-- group of rows sharing one timestamp, without naming a row.
--
-- THE DEFECT THIS CLOSES. sweep_cursors deliberately stores only sort-key timestamps and never a row
-- identity (see the table comment from 000140: it must outlive users erased by the very sweep that
-- advanced it). That is sound and stays. Its cost was that a capped run which stopped in the MIDDLE
-- of a tie group -- more rows sharing one timestamp than the run's cap -- had only two spellings for
-- "resume", both wrong: INCLUSIVE replays the identical page on every future run (starvation), and
-- EXCLUSIVE steps past the whole group, so the rows it never looked at are reached on no later run
-- AT ALL. 000140's own `exclusive` comment states that second cost plainly ("at the cost of skipping
-- the remainder of that one over-cap tie group"). For the accounts backfill -- which runs
-- SYNCHRONOUSLY at boot and repairs rows a known recurring write-path bug (#1306) keeps producing --
-- a permanent skip of repairable rows was judged unshippable.
--
-- WHAT ts1_offset IS. The number of candidate rows sharing ts1 that the owning run has already
-- consumed, in the sweep's own ORDER BY. The resume then reads "at or after ts1, skipping the first
-- ts1_offset of them" instead of "strictly after (ts1, some synthesized uuid)". That is an exact
-- intra-group position when the consumed prefix is still there, which is precisely the case that
-- matters: the rows that make a group over-cap and STICKY are the ones the sweep could not repair,
-- and those do not leave the candidate set.
--
-- ⚠ WHAT IT IS NOT, and this is the reason it is allowed to exist in this table at all: it is a
-- COUNT, not a row identity. It names no row, no user and no external entity; after an erasure it is
-- a number whose rows are gone, which is exactly the property the no-user_id/no-FK design asks for.
-- Do NOT "improve" it into a stored id.
--
-- ⚠ THE RESIDUAL, STATED PLAINLY. If rows in the consumed prefix LEAVE the candidate set between two
-- runs (they were repaired -- i.e. the sweep is doing its job), the offset overshoots and the next
-- run skips that many not-yet-visited rows. That skip is NOT permanent: a run that reaches the end
-- of its scan DELETEs its row, so the next run starts from the beginning and the skipped rows are at
-- a smaller offset. Repairs only ever shrink the candidate set, so this converges. The old exclusive
-- advance, by contrast, was permanent.
--
-- NULLABLE with no default, and no backfill: a row written by the previous code has no offset, and
-- NULL means "none", which the reader treats as 0 -- i.e. resume at the START of ts1's group. That is
-- the SAFE direction (re-read rows, never skip them) for any cursor row that survives the deploy.
--
-- Only the accounts backfill (backend/internal/database/backfill_accounts_cursor.go) writes this
-- column today. The four #1477 sweeps leave it NULL and are unaffected: their INSERT and SELECT name
-- their columns explicitly (repository/sweep_cursor_repository.go), so an added column is invisible
-- to them. They keep the residual described above; closing it for them is #1477's to do, not this
-- migration's.
--
-- Idempotent (IF NOT EXISTS). Squawk-clean: a nullable column with no default and no constraint is
-- a catalogue-only change and takes no table rewrite.
ALTER TABLE sweep_cursors ADD COLUMN IF NOT EXISTS ts1_offset INTEGER;

COMMENT ON COLUMN sweep_cursors.ts1_offset IS
    'Issue #1569: how many candidate rows sharing ts1 the owning run had already consumed, in that '
    'sweep''s own ORDER BY, so a resume can point INSIDE a tie group without storing a row identity. '
    'NULL means none -- resume at the start of ts1''s group, which is the safe direction. It is a '
    'COUNT, never an id: it names no row and no user, which is what lets it live in a table that '
    'deliberately carries no user_id and no foreign key. Written only by the accounts user_account '
    'backfill; the four periodic sweeps leave it NULL.';
