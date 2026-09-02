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
		require.Contains(t, body, "except where the law requires us to keep a record",
			"the %s must QUALIFY the categorical claim (#1581 review). Deletion is not total: "+
				"audit_logs.user_id is ON DELETE SET NULL and only the deletion-residue rows are "+
				"deleted, so ip_address, user_agent and the rest survive. An unqualified "+
				"\"everything\" is an erasure promise the deletion does not keep, and it also drops "+
				"the privacy policy's own carve-outs (anonymised retention, 90-day backups, legal "+
				"retention)", name)
		require.Contains(t, body, "there is no way to restore it",
			"the %s must say the deletion is irreversible — no restore path exists (#1569)", name)
		for _, item := range retentionDeletionItems {
			require.Contains(t, body, item.phrase,
				"the %s omits the enumerated item %q", name, item.phrase)
		}
	}

	// The item that claims audit_logs must say, in its own words, that those records are not
	// simply destroyed — found by the table it claims rather than by position, so reordering the
	// list cannot quietly unpin it. Without this, unqualifying the item passes every other
	// assertion here: the body-equality tests compare the sent body to this same rendered text, so
	// they move with it (verified by mutation — that is why this assertion exists).
	var auditItem string
	for _, item := range retentionDeletionItems {
		for _, tbl := range item.tables {
			if tbl == "audit_logs" {
				auditItem = item.phrase
			}
		}
	}
	require.NotEmpty(t, auditItem, "no enumerated item claims audit_logs")
	require.Contains(t, auditItem, "where we are required to keep a record",
		"the item claiming audit_logs promises destruction it does not deliver: audit_logs.user_id "+
			"is ON DELETE SET NULL and only the deletion-residue rows are deleted, so ip_address, "+
			"user_agent, request_id and the rest survive with resource_id still naming the user")

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
