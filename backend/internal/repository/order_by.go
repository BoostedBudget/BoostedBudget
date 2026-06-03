package repository

// orderByEntry maps a sort_by column name to a safe SQL ORDER BY expression.
// nullsLast appends "NULLS LAST" after the direction for nullable columns,
// ensuring NULLs always sort to the bottom regardless of ASC/DESC.
type orderByEntry struct {
	col       string
	nullsLast bool
}
