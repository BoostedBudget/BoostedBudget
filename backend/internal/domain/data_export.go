package domain

import "time"

// DataExportVersion is the schema version of the export document.
//
// It is part of the payload so that a consumer holding an old export can tell what shape it is
// looking at. Bump it whenever a section is renamed or removed, or whenever the meaning of an
// existing field changes; adding a new section or a new column inside a section is additive and
// does NOT require a bump (columns are carried verbatim from the live schema — see
// repository.DataExportTables).
const DataExportVersion = 1

// DataExport is the GDPR Article 20 ("right to data portability") / Article 15 ("right of access")
// payload for one user: a structured, commonly used, machine-readable representation of the data
// BoostedBudget holds about them.
//
// ⚠ SCOPE IS THE EXPORTING USER'S OWN DATA, NEVER A HOUSEHOLD PEER'S. Household sharing means a
// peer's rows are VISIBLE to this user inside the product, but visibility is not ownership: Article
// 20(4) says portability "shall not adversely affect the rights and freedoms of others". Every
// section is therefore scoped by an ownership predicate rooted at this user's own id, never through
// a household join. See repository.DataExportTables for the per-table predicate and
// TestExportUserData_Integration_DoesNotLeakAnotherHouseholdMembersData for the proof.
type DataExport struct {
	// ExportVersion is DataExportVersion at the time the export was produced.
	ExportVersion int `json:"export_version" doc:"Schema version of this export document"`

	// GeneratedAt is when the export was produced, in UTC.
	GeneratedAt time.Time `json:"generated_at" doc:"When this export was produced (UTC)"`

	// UserID is the exporting user's BoostedBudget id. It is the only identifier in the document:
	// the Clerk identifier is deliberately excluded (see repository.dataExportGlobalExcludedColumns).
	UserID string `json:"user_id" doc:"The exporting user's BoostedBudget identifier"`

	// Sections maps a live table name to that table's rows for this user. A table with no rows is
	// still present, with an empty list, so that a consumer can tell "you hold nothing here" apart
	// from "we forgot to include this".
	Sections map[string][]map[string]any `json:"sections" doc:"User-scoped rows, keyed by the source table name"`

	// ExcludedTables maps a table BoostedBudget holds but deliberately does NOT export to the reason
	// it is withheld. Publishing the exclusions in the export itself is what makes the exclusion
	// rule auditable by the data subject rather than only by us.
	ExcludedTables map[string]string `json:"excluded_tables" doc:"Tables deliberately not exported, and why"`

	// RowCount is the total number of rows across all sections. It is the number to watch when
	// deciding whether this endpoint must become an asynchronous job — see the sizing note on
	// DataExportService.
	RowCount int `json:"row_count" doc:"Total rows across all sections"`
}
