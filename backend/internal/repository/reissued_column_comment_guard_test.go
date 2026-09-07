package repository

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Issue #1569 T7-FIX, finding G — a re-issued COMMENT may not drift from the file it was copied from.
//
// THE FAILURE THIS EXISTS FOR. A migration file is immutable once applied, so correcting a false
// COMMENT ON COLUMN takes two edits: the ORIGINAL file (which is what a fresh database gets) and a
// NEW forward migration re-issuing the corrected text (which is what every already-migrated
// database gets). 000174 did exactly that and wrote "copied verbatim ... do not re-word it here
// independently of those files, or the two will drift apart again" — as prose, enforced by nobody.
//
// It did not survive one round. The sentence 000174 copied was itself false ("Timestamp when the
// user deliberately disconnected this account", when two of the three removal causes are not user
// actions at all), so the correction had to be made in THREE places: 000149, 000174 and the new
// 000175. Fixing one and leaving the identical sentence next door is how this defect kept
// surviving on this branch.
//
// WHAT THIS GUARD CLAIMS. For each column below, every UP migration that carries a
// `COMMENT ON COLUMN <table>.<column> IS <body>` must carry the SAME body, in either of the two
// quoting forms this tree uses — dollar-quoted `$COMMENT$…$COMMENT$` or one or more adjacent
// single-quoted literals, which SQL concatenates. "Same" means equal after whitespace is COLLAPSED
// (normaliseCommentText): re-wrapping a body across lines is not drift, but any change of word or
// punctuation is. It is NOT a byte comparison — do not describe it as one in a migration comment,
// which has already happened in five places at once — and the copies' indentation genuinely differs
// between a top-level statement and one inside a DO block. It says nothing about whether that text
// is TRUE — no test can — only that a correction cannot be applied to one copy and forgotten on the
// others, which is the mechanical half of the defect.
//
// WHAT IT DOES NOT CLAIM.
//
//   - ⚠ IT COMPARES BODIES, NOT TRUTH, AND NOT THE `--` NARRATIVE AROUND THEM — AND A NARRATIVE
//     TWIN OF A WATCHED BODY IS UNGUARDED BY CONSTRUCTION. These migrations restate their catalogue
//     text in prose directly above it, so a correction applied to the body and not to the twin four
//     lines up passes this guard cleanly. That is not hypothetical: 000141 and 000142 each kept the
//     retracted claim in their own narrative for a full round AFTER the body was fixed, and the
//     original "until the next wrap" defect lived ONLY in a `--` comment (000140's catalogue COMMENT
//     merely omitted the price), so this guard could never have caught it. When you correct a watched
//     body, grep the same file's prose for the sentence you just replaced — nothing HERE will.
//     ⚠ Partially mechanised since #1569 round 20 fix 3: TestNoRetractedSentenceSurvivesInNarrative
//     (retracted_sentence_guard_test.go) pins a HAND-LISTED table of outright-retracted sentences
//     and fails if one stands unquoted in any up migration's `--` prose. It catches only what is
//     enumerated, so it narrows this hole rather than closing it — a twin nobody lists is still
//     invisible. Do NOT try to close the rest by detecting English: an attempt at that class on this
//     branch produced ~70 hits of ordinary prose and was correctly discarded.
//   - It reads ONLY `*.up.sql`, and that is REQUIRED rather than a convenience. A comment-only DOWN
//     migration exists precisely to restore the SUPERSEDED wording, so including one would report a
//     deliberate, correct difference as drift. Three of the five columns below have exactly such a
//     down copy — sweep_cursors.ts2 (000141.down, 000184.down), sweep_cursors.exclusive (000142.down,
//     000184.down) and plaid_orphaned_items.token_expires_at (000136.down) — which is WHY the
//     restriction exists. What the restriction did NOT do is narrow what was already being compared:
//     every copy the guard counted before it was added lived in an up migration, so the accounts.*
//     counts (3 and 2) did not move.
//   - Both quoting forms ARE now seen, and the note that used to stand here — that a single-quoted
//     comment is invisible to this guard — was true until #1569 round 20 and is no longer. That
//     blind spot was one of the two reasons 000140's retracted comments went unwatched while they
//     were wrong; the other was simply that nobody had added them to the map below.
//     `accounts.replaced_at` is still deliberately absent from that map, but for the other half of
//     the old note: 000033 is its only copy, so there is nothing to drift.
//   - It un-escapes a doubled apostrophe on the single-quoted path but returns a dollar-quoted body
//     RAW, because dollar quoting has no escape to undo. A column written in BOTH forms would
//     therefore compare unequal wherever its text contains an apostrophe. Nothing does that today —
//     each watched column uses one form throughout — and the failure mode is a loud drift report,
//     not a silent pass, so it is documented rather than handled.
//   - It covers COMMENT ON COLUMN only. 000136 re-issues a COMMENT ON TABLE alongside its column
//     comment under the same "copied verbatim ... must never become one" instruction, and that
//     half is still enforced by prose alone.

// reissuedColumnComments names the columns whose COMMENT text is re-issued across migrations, with
// the number of copies expected. The count is a vacuity check: if a rename or a reformat makes the
// extractor stop seeing a file, the guard would otherwise pass by finding one copy and comparing it
// with itself — which is the real risk, since the mismatch itself fails loudly in EITHER direction.
// Re-derive a count by grepping `COMMENT ON COLUMN <col>` across `*.up.sql` rather than guessing.
var reissuedColumnComments = map[string]int{
	"accounts.removed_at": 3, // 000149 (origin), 000174 (first re-issue), 000175 (this correction)
	"accounts.expired_at": 2, // 000155 (origin), 000174
	// accounts.replaced_at is deliberately NOT here: 000033 is its only copy and 000174 does not
	// re-issue it, so there is nothing to drift.
	// 000184 re-issues BOTH of these, because 000141's and 000142's own replacement sentences were
	// later found wrong in turn (the ts2 text named one authority that was already not the only one;
	// the exclusive text stated the over-cap sacrifice with no precondition). All three files per
	// column are edited in lockstep and this guard is what makes that mandatory rather than hoped-for.
	"sweep_cursors.ts2":       3, // 000140 (origin), 000141 (first re-issue), 000184 (this correction)
	"sweep_cursors.exclusive": 3, // 000140 (origin), 000142 (first re-issue), 000184 (this correction)
	// 000135 (origin, corrected in place before it reached main) and 000136 (the catch-up for
	// databases that had applied 000135 already). 000136's header instructs that the bodies stay
	// verbatim copies "and must never become one [a summary]" — enforced by nobody until now.
	"plaid_orphaned_items.token_expires_at": 2,
	// 000065 (origin, corrected in place) and 000186 (the catch-up for databases that already
	// applied 000065). #1609 Item 11: the original text called the column a display hint that
	// "does not restrict usage", which was false the day it shipped (budget roll-up already
	// resolved through it) and became more so when the analytics transfer filter started doing
	// the same. See 000186's banner.
	"custom_categories.parent_pfc_primary": 2,
}

// columnCommentHeadRegexp matches the `COMMENT ON COLUMN <qualified column> IS` prefix; the body
// that follows is read by columnCommentsIn, which understands both quoting forms.
var columnCommentHeadRegexp = regexp.MustCompile(
	`(?is)COMMENT\s+ON\s+COLUMN\s+([a-z_][a-z0-9_.]*)\s+IS\s`)

type columnComment struct{ column, text string }

// columnCommentsIn extracts every `COMMENT ON COLUMN` body from RAW migration text, deliberately
// without blanking SQL comments first: these bodies contain `--` (they cite constraint names and
// aside clauses after a dash), so blanking would truncate a body at the first dash and compare two
// mutilated strings. The cost of matching raw text is that a `--` line QUOTING a whole COMMENT
// statement would be counted as a copy, so a statement whose line already contains `--` before it
// is skipped as prose. Write pointers in `--` comments as "the ts2 comment", never as a pasted
// `COMMENT ON COLUMN …` statement, and this stays true.
func columnCommentsIn(raw string) []columnComment {
	var out []columnComment
	for _, loc := range columnCommentHeadRegexp.FindAllStringSubmatchIndex(raw, -1) {
		lineStart := strings.LastIndexByte(raw[:loc[0]], '\n') + 1
		if strings.Contains(raw[lineStart:loc[0]], "--") {
			continue // a prose line quoting a statement, not a statement
		}
		column := strings.ToLower(raw[loc[2]:loc[3]])
		body, ok := readCommentBody(raw, loc[1])
		if !ok {
			continue
		}
		out = append(out, columnComment{column: column, text: normaliseCommentText(body)})
	}
	return out
}

const dollarCommentTag = "$COMMENT$"

// readCommentBody reads the string that follows `... IS` at offset i: either one dollar-quoted
// `$COMMENT$…$COMMENT$` literal, or a run of adjacent single-quoted literals (SQL concatenates
// those, and a DOUBLED apostrophe inside one is a single escaped apostrophe).
func readCommentBody(raw string, i int) (string, bool) {
	for i < len(raw) && isSQLSpace(raw[i]) {
		i++
	}
	if strings.HasPrefix(raw[i:], dollarCommentTag) {
		rest := raw[i+len(dollarCommentTag):]
		end := strings.Index(rest, dollarCommentTag)
		if end < 0 {
			return "", false
		}
		return rest[:end], true
	}

	var body strings.Builder
	literals := 0
	for {
		for i < len(raw) && isSQLSpace(raw[i]) {
			i++
		}
		if i >= len(raw) || raw[i] != '\'' {
			break
		}
		i++
		closed := false
		for i < len(raw) {
			if raw[i] != '\'' {
				body.WriteByte(raw[i])
				i++
				continue
			}
			if i+1 < len(raw) && raw[i+1] == '\'' { // escaped apostrophe
				body.WriteByte('\'')
				i += 2
				continue
			}
			i++
			closed = true
			break
		}
		if !closed {
			return "", false // unterminated literal: not something to compare
		}
		literals++
	}
	return body.String(), literals > 0
}

func isSQLSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\r' || b == '\n'
}

func TestReissuedColumnCommentsDoNotDrift(t *testing.T) {
	dir := filepath.Join(backendDirRoot(t), "migrations")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}

	type copyOf struct{ file, text string }
	copyFileNames := func(cs []copyOf) []string {
		names := make([]string, 0, len(cs))
		for _, c := range cs {
			names = append(names, c.file)
		}
		return names
	}
	found := map[string][]copyOf{}

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".up.sql") {
			continue
		}
		raw, readErr := os.ReadFile(filepath.Join(dir, e.Name()))
		if readErr != nil {
			t.Fatalf("reading %s: %v", e.Name(), readErr)
		}
		for _, c := range columnCommentsIn(string(raw)) {
			if _, watched := reissuedColumnComments[c.column]; !watched {
				continue
			}
			found[c.column] = append(found[c.column], copyOf{file: e.Name(), text: c.text})
		}
	}

	for col, wantCopies := range reissuedColumnComments {
		copies := found[col]
		if len(copies) != wantCopies {
			t.Errorf("expected %d up migration(s) to carry the COMMENT for %s, found %d %v.\n"+
				"A count that has GROWN means a new re-issue needs adding to reissuedColumnComments. "+
				"A count that has SHRUNK means the extractor stopped seeing a file, which would make "+
				"this guard compare a copy with itself and pass vacuously.",
				wantCopies, col, len(copies), copyFileNames(copies))
			continue
		}
		for i := 1; i < len(copies); i++ {
			if copies[i].text != copies[0].text {
				t.Errorf("the COMMENT on %s differs between %s and %s (#1569 T7-FIX finding G).\n"+
					"A re-issued comment must be copied VERBATIM from the corrected original: the "+
					"whole point of the re-issue is that already-migrated databases converge on the "+
					"same text a fresh one gets. Correcting one copy and leaving the other is how "+
					"the false 'user deliberately disconnected' claim survived two rounds.\n"+
					"  %s: %s\n  %s: %s",
					col, copies[0].file, copies[i].file,
					copies[0].file, copies[0].text, copies[i].file, copies[i].text)
			}
		}
	}
}

// normaliseCommentText collapses whitespace so that re-wrapping a long comment across lines is not
// reported as drift. Line breaks inside these comments are typography, not content.
func normaliseCommentText(s string) string {
	return strings.TrimSpace(sqlWhitespaceRegexp.ReplaceAllString(s, " "))
}
