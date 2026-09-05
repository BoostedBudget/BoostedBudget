package workers

// ptrS is a test-only helper for the *string fields introduced by Issue #1569 Task 14
// (domain.BoostExecution.BoostID became a pointer when fk_boost_executions_boost_id was flipped to
// ON DELETE SET NULL).
func ptrS(s string) *string { return &s }
