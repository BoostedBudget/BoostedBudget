package annotationguard

import "context"

// UnguardedTagsWrite is #1569 gap G12, constructed. Until tags/tags_source entered
// annotationCapturedColumns, the guard's tracked-column universe did not contain them, so a
// whole-array replace of the user's tags — the one write that can destroy a tag set with no other
// copy anywhere — was not merely excused, it was INVISIBLE. UpdateTags itself sat in this hole for
// the whole of #1579, in neither the captured set nor the exemption list.
func UnguardedTagsWrite(ctx context.Context, d db, id, userID string) error {
	query := `
		UPDATE transactions
		SET tags = $1, tags_source = $2, updated_at = $3
		WHERE id = $4 AND user_id = $5
	`
	_, err := d.Exec(ctx, query, "[]", "[]", "now", id, userID)
	return err
}
