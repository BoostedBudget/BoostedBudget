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
