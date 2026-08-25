package jobs

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestRetentionNoticeBodiesCarryTheWholeEnumeration is the schema-free half of the #1581 guard.
//
// TestRetentionNoticeEnumerationCoversEverythingDeleted (integration-tagged) decides the hard
// question — whether the enumeration still covers everything DeleteUserCascade destroys — because
// only the live schema knows that. This half runs everywhere and pins the properties that need no
// database: both notices carry every phrase, both say plainly that the deletion is categorical and
// irreversible, and no phrase claims a table another phrase already claimed.
//
// "there is no way to restore it" is asserted, not decorative. Nothing in this codebase restores a
// deleted user, and nothing sets deleted_at back to NULL; a warning that leaves that ambiguous
// invites the reader to assume a grace period that does not exist.
func TestRetentionNoticeBodiesCarryTheWholeEnumeration(t *testing.T) {
	require.NotEmpty(t, retentionDeletionItems)

	bodies := map[string]string{
		"30-day notice": retentionNotice30dBody,
		"7-day notice":  retentionNotice7dBody,
	}
	for name, body := range bodies {
		require.Contains(t, body, "everything in your BoostedBudget account",
			"the %s must make the categorical claim before the itemised list, so the list reads as "+
				"an illustration of everything rather than as its boundary", name)
		require.Contains(t, body, "there is no way to restore it",
			"the %s must say the deletion is irreversible — no restore path exists (#1569)", name)
		for _, item := range retentionDeletionItems {
			require.Contains(t, body, item.phrase,
				"the %s omits the enumerated item %q", name, item.phrase)
		}
	}

	claimedBy := map[string]string{}
	for _, item := range retentionDeletionItems {
		require.NotEmpty(t, strings.TrimSpace(item.phrase))
		require.NotEmpty(t, item.tables, "item %q claims no tables", item.phrase)
		for _, tbl := range item.tables {
			prev, dup := claimedBy[tbl]
			require.False(t, dup, "table %q is claimed by both %q and %q", tbl, prev, item.phrase)
			claimedBy[tbl] = item.phrase
		}
	}
}
