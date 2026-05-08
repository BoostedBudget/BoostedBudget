// Package sql provides reusable SQL fragments and constants for account
// status filtering. Centralising these strings prevents callers from
// hand-rolling status checks and ensures consistent handling of the
// re-link account chain across all query sites.
package sql

import "fmt"

// WhereAccountActive is an AND clause fragment that restricts a query to
// active accounts only. Append to an existing WHERE clause; the leading
// space and AND keyword are intentional so callers can concatenate directly.
//
// Example:
//
//	query := "SELECT id FROM accounts WHERE user_id = $1" + sql.WhereAccountActive
const WhereAccountActive = " AND status = 'active'"

// WhereAliasedAccountActive is the aliased form of WhereAccountActive.
// Use when the accounts table is referenced via an alias in a JOIN query.
//
// Example:
//
//	query := "SELECT a.id FROM accounts a LEFT JOIN ... WHERE a.user_id = $1" +
//	         sql.WhereAliasedAccountActive("a")
func WhereAliasedAccountActive(alias string) string {
	return fmt.Sprintf(" AND %s.status = 'active'", alias)
}
