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
--
-- ⚠ WITH ONE PRECONDITION, WHICH THIS PARAGRAPH LEFT IMPLICIT: "stops in the same place" holds only
-- while that first cap-worth keeps FAILING, since failing is what makes those head rows keep
-- consuming the whole cap. The moment they succeed they leave the candidate set, the tail behind
-- them becomes the new head, and it is reached. Nothing above is withdrawn -- "until the next wrap"
-- is still false, because a wrap does not repair the case where the head keeps failing -- but the
-- omission is named here rather than 25 lines down, because the identical omission in the catalogue
-- body was corrected and this restatement of it was not.
--
-- That is precisely why the advance goes exclusive in the first place: an inclusive resume would
-- replay the identical page forever. The old text therefore promised a bound that the mechanism it
-- describes cannot deliver, and a reader budgeting for "one extra cycle of latency" would have been
-- reading a permanent omission as a delay. repository.NextPosition states the same price correctly
-- and has done since it was written; it was only 000140's DOCUMENTATION that was wrong, never the
-- code. ⚠ Read "documentation" precisely, because this line has been misread as a narrower claim:
-- the false sentence lived in 000140's `--` NARRATIVE only. Its catalogue COMMENT did not carry that
-- sentence at all -- it simply omitted the price, which is why the replacement below is so much
-- longer than the text it replaces rather than a like-for-like retraction of it.
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
-- ⚠ THIS FILE'S OWN REPLACEMENT TEXT WAS LATER FOUND INCOMPLETE AND IS CORRECTED IN PLACE BELOW.
-- It stated the over-cap sacrifice with NO precondition ("...are then reached on no run at all"),
-- which overstates it: the sacrifice lasts only while the group's first cap-worth keeps FAILING and
-- so keeps consuming the whole cap; when those head rows succeed they leave the candidate set and
-- the tail becomes the new head. repository.NextPosition carries this precondition -- and flags that
-- its own earlier wording got it wrong and was corrected in place ("AND THE PRECONDITION IS NOT WHAT
-- THIS COMMENT USED TO SAY"), so it is not a source that was right all along either. Migration 000184
-- re-issues the corrected body for databases that had already applied this file.
-- ⚠ The SAME un-preconditioned form also stood in this file's own NARRATIVE above and was missed on
-- the first correction pass, because the guard compares catalogue BODIES and never narrative. It is
-- marked there too, in place.
-- TestReissuedColumnCommentsDoNotDrift holds 000140, this file and 000184 in agreement -- comparing
-- the bodies with whitespace COLLAPSED, so re-wrapping is not drift but any change of word or
-- punctuation is. It is not a byte comparison, and the three files' indentation already differs.
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
        'cap-worth and stops in the same place. THE PRECONDITION IS THE HEAD, NOT THE GROUP, and this '
        'comment used to omit it: the sacrifice lasts only while that first cap-worth keeps FAILING and '
        'so keeps consuming the whole cap. The moment those head rows succeed they leave the candidate '
        'set, the tail behind them becomes the new head, and it is reached. 000140 said the sacrificed '
        'rows were reached "until the next wrap"; that recovery does not exist and this comment '
        'retracts it. Everything RANKED BEHIND the group is reached unconditionally, which is what the '
        'exclusive advance buys. The authoritative statement, with the precondition and the test that '
        'constructs it, is repository.NextPosition.';
    END IF;
END $$;
