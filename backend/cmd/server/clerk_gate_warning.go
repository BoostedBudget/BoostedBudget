package main

// Issue #1450 — the startup warning emitted when a database is available but CLERK_SECRET_KEY is
// not, which silently disables every background job.
//
// WHY THIS IS A HELPER RATHER THAN AN INLINE appLogger.Warn IN main().
//
// The first version of this warning lived inline, and its test scanned main.go as text for a
// `Warn(` call plus some keywords within a byte window after an anchor string — matching this
// package's pre-existing drift-test convention. A Codex adversarial review pointed out the obvious
// problem: such a test can pass after the production warning is altered or deleted, as long as
// nearby text happens to match, and it can never prove the warning is emitted from the intended
// BRANCH. A test that survives deletion of the thing it guards is worse than no test, because it
// reads as coverage.
//
// So the decision and the message now live here, where a test can call them directly and assert on
// the real return values across the full truth table. main() keeps only the logging call. This is
// the same move Issue #1448 made for the periodic-job registrations (buildPeriodicJobSpecs in
// periodic_jobs.go): extract an inspectable value, then assert on it structurally.

// clerkGateWarningMessage is the warning's headline. Exported as a constant so the test asserts the
// exact string main() logs rather than a substring that might drift apart from it.
const clerkGateWarningMessage = "CLERK_SECRET_KEY is unset — ALL background jobs are disabled, " +
	"including GDPR-mandated processing"

// clerkGateWarning reports whether the Clerk-gate warning is warranted, and returns the message and
// structured attributes to log when it is.
//
// The gate in main() is `db != nil && cfg.ClerkSecretKey != ""`. There are three distinct cases and
// only one of them warrants a warning:
//
//   - dbPresent && clerkConfigured — the gate is OPEN; background jobs are registered. No warning.
//   - dbPresent && !clerkConfigured — the gate is CLOSED **despite a usable database**. This is the
//     case Issue #1450 is about: the jobs could have run, and are silently not running.
//   - !dbPresent — there is no database at all, so background processing was never on the table.
//     main() already reports the absent database elsewhere; a Clerk-specific warning here would be
//     noise that misattributes the cause.
//
// The consequences are enumerated as individual structured attributes rather than prose so they
// survive log aggregation and can be alerted on individually. Each names a concrete subsystem,
// because main()'s startup summary already logs `"clerk", boolStatus(...)` as a bare status flag —
// that line does not say what breaks, which is exactly why #1450 was filed.
func clerkGateWarning(dbPresent, clerkConfigured bool) (msg string, attrs []any, warranted bool) {
	if !dbPresent || clerkConfigured {
		return "", nil, false
	}

	return clerkGateWarningMessage, []any{
		"consequence_gdpr_pre_deletion_notices", "NOT running (30-day and 7-day warnings)",
		"consequence_account_deletion", "NOT running (deleteExpiredAccounts never executes)",
		"consequence_retention_clock_repair", "NOT running",
		"consequence_plaid_disconnect_repair", "NOT running",
		"remediation", "set CLERK_SECRET_KEY, or see Issue #1450 for narrowing the gate to jobs that don't need Clerk",
	}, true
}
