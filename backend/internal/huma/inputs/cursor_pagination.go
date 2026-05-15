package inputs

// CursorPaginationInput is the pagination input for cursor-based (keyset) pagination.
// Used by the transactions list endpoint instead of the shared PaginationInput.
//
// Cursor is an opaque token returned by the previous page as next_cursor.
// Omit it (or pass empty string) to request the first page.
type CursorPaginationInput struct {
	Cursor string `query:"cursor" doc:"Opaque cursor from previous page's next_cursor; omit for first page"`
	Limit  int    `query:"limit" minimum:"1" maximum:"100" default:"50"`
}
