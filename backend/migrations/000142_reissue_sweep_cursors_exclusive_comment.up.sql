-- Issue #1477 follow-up: RE-ISSUE the COMMENT on sweep_cursors.exclusive, because 000140 documented a
-- RECOVERY THAT DOES NOT EXIST.
--
-- THE CLAIM BEING RETRACTED, RESTATED IN FULL SO THIS FILE IS NOT A DANGLING POINTER. 000140's
-- narrative for the exclusive column ended:
--
--     "It buys guaranteed forward progress at the cost of skipping the remainder of that one
--      over-cap tie group until the next wrap."
--
-- "Until the next wrap" is false. A wrap restarts the sweep at the BEGINNING of its candidate set,
-- which re-reads the same first cap-worth of the same tie group and stops in the same place, so the
-- rows behind that first cap-worth are reached on NO run -- not a later one, and not a post-wrap one.
-- That is precisely why the advance goes exclusive in the first place: an inclusive resume would
-- replay the identical page forever. The old text therefore promised a bound that the mechanism it
-- describes cannot deliver, and a reader budgeting for "one extra cycle of latency" would have been
-- reading a permanent omission as a delay. repository.NextPosition states the same price correctly
-- and has done since it was written; it was only the catalogue-facing wording that was wrong.
--
-- WHY A NEW FILE RATHER THAN AN EDIT. 000140 has already been applied to running databases and
-- golang-migrate never re-runs an applied version, so editing it in place would leave every QA and
-- staging catalogue carrying the false recovery claim forever. This is the same catch-up 000136 was
-- for 000135's overstated retention comments, and 000141 was for 000140's own ts2 comment.
--
-- THE NEW WORDING DESCRIBES A CONDITION, NOT A MECHANISM OR A COUNT, AND THAT IS DELIBERATE. 000141
-- existed because 000140 enumerated sweeps BY NUMBER ("the two user sweeps") and a fourth sweep made
-- the count wrong. The failure repeated here has the same shape one level over: 000140 named a future
-- EVENT ("the next wrap") as the thing that repairs the omission, so the sentence had to be re-checked
-- against the wrap's behaviour to notice it was wrong, and nothing in it looked stale. The replacement
-- below says only WHICH ROWS ARE AFFECTED and WHAT IS AND IS NOT TRUE OF THEM -- a property of the
-- position that stays true however many sweeps exist, whatever their caps are, and whether or not the
-- wrap, the cap constants or the candidate predicates are later changed.
--
-- SCOPE: one comment. No column, constraint, index, default or datum is touched, and no behaviour
-- changes -- the mechanism was always correct, only its description was not. Squawk: this repo runs
-- squawk with excluded_rules set, so a clean squawk report here would be weak evidence and is not
-- being leaned on. Reasoned instead from the operation: COMMENT ON COLUMN takes only the ordinary
-- catalogue lock on the relation, rewrites no heap or index, adds no constraint, sets no default and
-- validates nothing, so every rule this repo excludes (require-concurrent-index-creation,
-- adding-not-nullable-field, adding-foreign-key-constraint, disallowed-unique-constraint, ...) is
-- INAPPLICABLE to this statement rather than merely silenced by configuration.
--
-- Idempotent: COMMENT is inherently repeatable (setting the same comment twice leaves the same
-- comment), and the statement is guarded on the column still existing, so this is a clean no-op rather
-- than an error on a database where 000140 was rolled back.

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_schema = current_schema()
          AND table_name = 'sweep_cursors'
          AND column_name = 'exclusive'
    ) THEN
        COMMENT ON COLUMN sweep_cursors.exclusive IS
        'FALSE = resume AT this timestamp key, re-visiting the rows that tie on it and skipping none. '
        'TRUE = resume STRICTLY AFTER it, and it is set in exactly one condition: the run started from '
        'a stored key and spent its whole cap without the timestamp key changing, so an inclusive save '
        'would hand the next run the identical page forever. THE PRICE, STATED AS A CONDITION SO IT '
        'CANNOT BE MISREAD AS A DELAY: whichever rows sit behind the first cap-worth of that one '
        'identical-timestamp-key group are then reached on no run at all -- not on a later run, and '
        'not by restarting the sweep from the beginning, because restarting re-reads that same first '
        'cap-worth and stops in the same place. 000140 said those rows were reached "until the next '
        'wrap"; that recovery does not exist and this comment retracts it. Everything RANKED BEHIND '
        'the group is reached, which is what the exclusive advance buys. The authoritative statement, '
        'with the test that demonstrates it, is repository.NextPosition.';
    END IF;
END $$;
