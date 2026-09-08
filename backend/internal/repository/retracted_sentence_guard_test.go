package repository

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Issue #1569 round 20 fix 3 — a sentence a migration RETRACTED may not still stand as an assertion
// in any migration's `--` narrative.
//
// THE HOLE THIS CLOSES, AND WHY THE OTHER GUARD CANNOT.
// TestReissuedColumnCommentsDoNotDrift compares catalogue COMMENT BODIES. These migrations restate
// their catalogue text in prose directly above it, so a correction applied to the body and not to
// the twin four lines up passes that guard cleanly. That is not hypothetical and it is not rare: it
// happened THREE times in two rounds. Run against the tree as it stood before each fix, the table
// below flags both of the sweep_cursors instances — 000140's "until the next wrap" recovery at
// ae8e7335, and 000141's "the one place ..." twin at 854e29ce, which survived a whole round of
// correcting the body next to it.
//
// WHAT IT DOES. For each hand-listed sentence, assert it appears in NO `--` line of any `*.up.sql`
// EXCEPT inside a double-quoted span. Matching is on comment-stripped, whitespace-collapsed,
// CASE-INSENSITIVE text — case-insensitive because a case-sensitive grep for `byte-identical`
// missed two of five copies in this very file's history, which is how a false claim about the other
// guard's strength shipped in five places at once.
//
// THE EXEMPTION IS QUOTATION, NOT VOCABULARY, AND THAT CHOICE IS LOAD-BEARING.
// A retraction has to restate what it retracts or it is a dangling pointer, so these files quote the
// dead sentence deliberately and that must stay legal. The rule is: an occurrence is legal iff an
// ODD number of `"` characters precedes it within its own contiguous run of `--` lines — i.e. it
// sits inside a quoted span. The window is that contiguous run and nothing wider; a blank line or
// any SQL statement ends it.
//
// A marker-vocabulary rule ("CORRECTED IN PLACE", "RESTATED IN FULL", … within N lines above) was
// built first and REJECTED on measurement: two of the eight legitimate quotations in the tree carry
// no such vocabulary anywhere near them (000141's own "WHY THIS FILE EXISTS AT ALL" paragraph, and
// 000175's opening), so it produced two false positives on day one. A guard that cries wolf about
// correct retraction prose is a guard someone deletes. Quotation parity produces ZERO false
// positives across all eight and still flags both real defects.
//
// WHAT IT DOES NOT CLAIM.
//
//   - ⚠ IT IS HAND-MAINTAINED AND MAKES NO CLAIM OF COMPLETENESS. A retracted sentence nobody adds
//     to the table below is simply unchecked — exactly the coverage that exists today, never worse.
//     It FAILS SAFE by construction: it can only ever report a sentence someone deliberately listed,
//     so it cannot invent a failure. This is the same weakness the PII-sweep membership list and the
//     export registry carry, and it is stated here rather than implied away.
//   - It is not English detection, and must not become it. An earlier attempt at that class on this
//     branch produced ~70 hits of ordinary English and was correctly discarded.
//   - KNOWN BYPASS: re-asserting a dead claim INSIDE quotation marks passes. Quoting is this tree's
//     convention for citing, not asserting, so the exemption is right; the bypass is the price.
//   - It reads `--` NARRATIVE only, never string literals. A retracted fragment can legitimately
//     survive inside a corrected catalogue body that quotes it (000141's body quotes "NULL for the
//     two user sweeps"), and the body's own agreement is the other guard's job.
//   - Only sentences retracted OUTRIGHT belong here. 000142's un-preconditioned "reached on no run
//     at all" was AMENDED — the claim still holds under a stated precondition — so no wording of it
//     is unconditionally illegal and it is deliberately absent.

type retractedSentence struct {
	column      string // the watched column whose COMMENT carried it, for cross-reference
	retractedBy string // the migration that retracted it
	sentence    string // the exact dead text, as it was written
	why         string // one line: what was wrong with it
}

// retractedSentences is the hand-enumerated table. Add an entry when a migration retracts a claim
// outright; quote the dead sentence from the retraction's own banner rather than reconstructing it.
var retractedSentences = []retractedSentence{
	{
		column:      "sweep_cursors.exclusive",
		retractedBy: "000142",
		sentence: "It buys guaranteed forward progress at the cost of skipping the remainder of " +
			"that one over-cap tie group until the next wrap.",
		why: "the wrap restarts at the beginning and re-reads the same first cap-worth, so that " +
			"recovery does not exist; a reader budgeting for one extra cycle read a permanent " +
			"omission as a delay",
	},
	{
		column:      "sweep_cursors.ts2",
		retractedBy: "000141",
		sentence:    "NULL for the two user sweeps",
		why: "a fourth sweep made the SET wrong the day it was added; the replacement states a " +
			"CONDITION rather than a count so the next sweep cannot invalidate it again",
	},
	{
		column:      "sweep_cursors.ts2",
		retractedBy: "000184",
		sentence: "The authoritative per-sweep mapping is repository.SweepCursorName, whose " +
			"constants each name their own TS1/TS2 columns and which a new sweep has to edit anyway.",
		why: "a fifth writer already does not edit that type — backfill_accounts_cursor.go writes " +
			"accounts_user_account_backfill with ts2 NULL, for an import-cycle reason",
	},
	{
		column:      "sweep_cursors.ts2",
		retractedBy: "000184",
		sentence: "the one place where the per-sweep TS1/TS2 mapping is enumerated and where a new " +
			"sweep must add itself anyway",
		why: "the NARRATIVE twin of the entry above. It outlived the body's correction by a full " +
			"round, which is the failure this whole guard exists for",
	},
	{
		column:      "custom_categories.parent_pfc_primary",
		retractedBy: "000186",
		sentence: "Optional PFCv2 primary hint for icon/color fallback resolution. Does not " +
			"restrict usage.",
		why: "it restricted usage from the day it shipped -- categoryMatchClause rolls a " +
			"PFC-primary budget up through this column -- and #1609 made it decide transfer " +
			"classification in analytics as well",
	},
	{
		column:      "accounts.removed_at",
		retractedBy: "000175",
		sentence:    "Timestamp when the user deliberately disconnected this account",
		why: "two of the three removal causes are not user actions at all (provider_absent, " +
			"link_removed)",
	},
}

// commentBlock is one contiguous run of `--` lines: the normalised text, plus enough index to map a
// match offset back to a source line.
type commentBlock struct {
	text  string // whitespace-collapsed, lowercased, `--` markers stripped
	ends  []int  // ends[i] = length of text after appending line i
	lines []int  // lines[i] = 1-based source line number of line i
}

func commentBlocksIn(raw string) []commentBlock {
	var blocks []commentBlock
	var cur commentBlock
	flush := func() {
		if len(cur.lines) > 0 {
			blocks = append(blocks, cur)
		}
		cur = commentBlock{}
	}
	for i, line := range strings.Split(raw, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "--") {
			flush()
			continue
		}
		cur.text = normaliseCommentText(cur.text + " " + strings.ToLower(strings.TrimSpace(trimmed[2:])))
		cur.ends = append(cur.ends, len(cur.text))
		cur.lines = append(cur.lines, i+1)
	}
	flush()
	return blocks
}

func (b commentBlock) lineAt(offset int) int {
	for i, end := range b.ends {
		if end > offset {
			return b.lines[i]
		}
	}
	return b.lines[len(b.lines)-1]
}

// quoted reports whether offset sits inside a double-quoted span of this block.
func (b commentBlock) quoted(offset int) bool {
	return strings.Count(b.text[:offset], `"`)%2 == 1
}

func TestNoRetractedSentenceSurvivesInNarrative(t *testing.T) {
	dir := filepath.Join(backendDirRoot(t), "migrations")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}

	needles := make([]string, len(retractedSentences))
	for i, r := range retractedSentences {
		needles[i] = normaliseCommentText(strings.ToLower(r.sentence))
		if needles[i] == "" {
			t.Fatalf("retractedSentences[%d] (%s) has an empty sentence; an empty needle would "+
				"match everything", i, r.column)
		}
	}

	citations := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".up.sql") {
			continue
		}
		raw, readErr := os.ReadFile(filepath.Join(dir, e.Name()))
		if readErr != nil {
			t.Fatalf("reading %s: %v", e.Name(), readErr)
		}
		for _, block := range commentBlocksIn(string(raw)) {
			for i, needle := range needles {
				for at := 0; ; {
					p := strings.Index(block.text[at:], needle)
					if p < 0 {
						break
					}
					p += at
					at = p + 1
					if block.quoted(p) {
						citations++
						continue
					}
					r := retractedSentences[i]
					t.Errorf("%s:%d states a RETRACTED sentence as its own assertion, outside "+
						"quotation marks (#1569 round 20 fix 3).\n"+
						"  sentence:     %q\n"+
						"  retracted by: %s, for %s\n"+
						"  why it went:  %s\n"+
						"Correcting the catalogue COMMENT and leaving its narrative twin standing is "+
						"how this defect kept surviving: TestReissuedColumnCommentsDoNotDrift "+
						"compares BODIES and cannot see prose. Either correct this sentence too, or "+
						"— if you are deliberately CITING the dead text in a retraction banner — put "+
						"it in double quotes, which is this tree's convention for citing rather than "+
						"asserting.",
						e.Name(), block.lineAt(p), r.sentence, r.retractedBy, r.column, r.why)
				}
			}
		}
	}

	// Vacuity check. Every sentence in the table is quoted by the very migration that retracted it,
	// so a scan that finds NO legal citation at all has stopped seeing the corpus — a renamed
	// directory, a changed suffix filter, or a normalisation that no longer matches the files.
	if citations < len(retractedSentences) {
		t.Errorf("found only %d legal (quoted) citation(s) of %d retracted sentence(s). Each is "+
			"restated by its own retraction banner, so fewer citations than sentences means the "+
			"scan stopped seeing the migrations and this guard would pass vacuously.",
			citations, len(retractedSentences))
	}
}
