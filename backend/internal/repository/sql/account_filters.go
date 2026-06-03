package accountsql

import "fmt"

// WhereAccountActive filters to accounts with status='active' using the standard 'a' alias.
// This assumes the accounts table is aliased as 'a' in the query (e.g., "FROM accounts a").
// Use WhereAliasedAccountActive when a different alias is needed.
const WhereAccountActive = " AND a.status = 'active'"

// WhereAliasedAccountActive returns a status='active' filter using the given table alias.
func WhereAliasedAccountActive(alias string) string {
	return fmt.Sprintf(" AND %s.status = 'active'", alias)
}

// ChainExpansionWhere returns a WHERE clause that matches an account ID and all accounts
// in its re-link chain via user_account_id.
//
// Used by repositories that do NOT have a user_account_id column (e.g., liabilities).
// For repositories with user_account_id, use the direct form:
//   WHERE user_account_id = (SELECT user_account_id FROM accounts WHERE id = $N AND user_account_id IS NOT NULL)
//        OR (account_id = $N AND user_account_id IS NULL)
//
// paramIdx is the 1-based index of the $N placeholder holding the account ID.
// col is the column name to match (e.g., "account_id").
func ChainExpansionWhere(col string, paramIdx int) string {
	return fmt.Sprintf(`
	WHERE %[1]s = $%[2]d
	   OR %[1]s IN (
	        SELECT id FROM accounts
	        WHERE user_account_id = (
	            SELECT user_account_id FROM accounts WHERE id = $%[2]d AND user_account_id IS NOT NULL
	        )
	   )`, col, paramIdx)
}
