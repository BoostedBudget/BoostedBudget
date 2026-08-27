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
// WHAT THIS GUARD CLAIMS. For each column below, every migration that carries a
// `COMMENT ON COLUMN <table>.<column> IS $COMMENT$...$COMMENT$` must carry the SAME text. It says
// nothing about whether that text is TRUE — no test can — only that a correction cannot be applied
// to one copy and forgotten on the others, which is the mechanical half of the defect.
//
// WHAT IT DOES NOT CLAIM. It only sees the dollar-quoted `$COMMENT$` form. A comment written as an
// ordinary single-quoted string is invisible to it; the accounts lifecycle comments all use the
// dollar-quoted form, and the vacuity check below fails if that stops being true.

// reissuedColumnComments names the columns whose COMMENT text is re-issued across migrations, with
// the number of copies expected. The count is a vacuity check: if a rename or a reformat makes the
// extractor stop seeing a file, the guard would otherwise pass by finding one copy and comparing it
// with itself.
var reissuedColumnComments = map[string]int{
	"accounts.removed_at": 3, // 000149 (origin), 000174 (first re-issue), 000175 (this correction)
	"accounts.expired_at": 2, // 000155 (origin), 000174
	// accounts.replaced_at is deliberately NOT here: 000033 writes it as an ordinary single-quoted
	// string and 000174 does not re-issue it, so there is exactly one copy and nothing to drift.
}

// dollarQuotedColumnCommentRegexp matches `COMMENT ON COLUMN <qualified column> IS $COMMENT$…$COMMENT$`.
var dollarQuotedColumnCommentRegexp = regexp.MustCompile(
	`(?is)COMMENT\s+ON\s+COLUMN\s+([a-z_][a-z0-9_.]*)\s+IS\s+\$COMMENT\$(.*?)\$COMMENT\$`)

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
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		raw, readErr := os.ReadFile(filepath.Join(dir, e.Name()))
		if readErr != nil {
			t.Fatalf("reading %s: %v", e.Name(), readErr)
		}
		// Matched against the RAW text, deliberately. These comment bodies contain `--` (they cite
		// constraint names after a dash), so blanking SQL comments first would truncate the body at
		// the first dash and compare two mutilated strings. The cost is that a `--` line quoting a
		// whole dollar-quoted COMMENT statement would be counted; nothing in the tree does that, and
		// the per-column copy count above would fail loudly if one appeared.
		for _, m := range dollarQuotedColumnCommentRegexp.FindAllStringSubmatch(string(raw), -1) {
			col := strings.ToLower(m[1])
			if _, watched := reissuedColumnComments[col]; !watched {
				continue
			}
			found[col] = append(found[col], copyOf{file: e.Name(), text: normaliseCommentText(m[2])})
		}
	}

	for col, wantCopies := range reissuedColumnComments {
		copies := found[col]
		if len(copies) != wantCopies {
			t.Errorf("expected %d migration(s) to carry the COMMENT for %s, found %d %v.\n"+
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
