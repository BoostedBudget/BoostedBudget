//go:build integration

package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// Issue #1569, Task 16 (F3). The MCP capability manifest gained a third class, `destructive`,
// for operations that permanently destroy stored rows. mcp_audit_log.operation_class was
// CHECK-constrained to ('read','write') by migration 000084, so an agent's deleteTag was logged
// indistinguishably from its createTag — in the one place an operator would go to ask whether an
// agent destroyed anything. Migration 000166 widens the constraint.
//
// This is an INTEGRATION test on purpose. The repository's INSERT is exercised by a pgxmock unit
// test, and pgxmock cannot see a CHECK constraint at all: it would have passed with 'destructive'
// against migration 000084's schema and told us nothing.
//
// FALSIFIED against a database migrated only to 000165: the first case fails with
// `new row for relation "mcp_audit_log" violates check constraint
// "mcp_audit_log_operation_class_check" (SQLSTATE 23514)`.
func TestMCPAuditLog_AcceptsDestructiveOperationClass_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	repo := NewMCPAuditRepository(pool)

	actor := uuid.NewString()
	t.Cleanup(func() {
		// NOT t.Context(): a cancelled context here leaves fixture rows behind on the shared DB.
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM mcp_audit_log WHERE actor_user_id = $1`, actor)
	})

	for _, class := range []string{"read", "write", "destructive"} {
		t.Run(class, func(t *testing.T) {
			err := repo.Record(ctx, &domain.MCPAuditEntry{
				ActorUserID:    actor,
				GrantID:        "grant-" + class,
				OperationClass: class,
				OperationID:    "purgeSavingsGoal",
				Result:         "denied",
			})
			require.NoError(t, err, "operation_class %q must be recordable", class)
		})
	}

	var got []string
	rows, err := pool.Query(ctx,
		`SELECT operation_class FROM mcp_audit_log WHERE actor_user_id = $1 ORDER BY operation_class`, actor)
	require.NoError(t, err)
	defer rows.Close()
	for rows.Next() {
		var c string
		require.NoError(t, rows.Scan(&c))
		got = append(got, c)
	}
	require.NoError(t, rows.Err())
	assert.Equal(t, []string{"destructive", "read", "write"}, got)
}

// The widened constraint must still REJECT anything outside the vocabulary — widening it to three
// values is not the same as dropping it, and a migration that accidentally removed the CHECK
// would leave the test above green.
func TestMCPAuditLog_StillRejectsUnknownOperationClass_Integration(t *testing.T) {
	pool := connectTestDB(t)
	ctx := context.Background()
	repo := NewMCPAuditRepository(pool)

	actor := uuid.NewString()
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM mcp_audit_log WHERE actor_user_id = $1`, actor)
	})

	err := repo.Record(ctx, &domain.MCPAuditEntry{
		ActorUserID:    actor,
		GrantID:        "grant-bogus",
		OperationClass: "catastrophic",
		OperationID:    "purgeSavingsGoal",
		Result:         "denied",
	})
	require.Error(t, err)
	assert.Contains(t, fmt.Sprint(err), "mcp_audit_log_operation_class_check")
}
