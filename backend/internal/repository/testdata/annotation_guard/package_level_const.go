package annotationguard

import "context"

// zzPackageLevelUnguardedQuery is review fix round 2's finding D-1, constructed. The guard used to
// run ast.Inspect over function bodies only, so a file-scope const holding the SQL was never
// scanned at all — and file-scope SQL constants are this package's own idiom.
const zzPackageLevelUnguardedQuery = `
	UPDATE transactions t
	SET reviewed = true,
	    reviewed_at = NOW()
	WHERE t.id = $1
`

// UsePackageLevelQuery executes it, so the const is not dead code that a reader would delete.
func UsePackageLevelQuery(ctx context.Context, d db, id string) error {
	_, err := d.Exec(ctx, zzPackageLevelUnguardedQuery, id)
	return err
}
