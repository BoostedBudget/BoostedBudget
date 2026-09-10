package repository

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// DB is the interface for database operations that is satisfied by both
// *pgxpool.Pool and pgxmock.PgxPoolIface. This allows for easy testing
// with mock databases.
type DB interface {
	Begin(ctx context.Context) (pgx.Tx, error)
	Exec(ctx context.Context, sql string, arguments ...interface{}) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...interface{}) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...interface{}) pgx.Row
	SendBatch(ctx context.Context, b *pgx.Batch) pgx.BatchResults
}

// Querier is the read/write surface shared by *pgxpool.Pool, pgx.Tx and pgxmock. A repository
// method that must be able to run either standalone or INSIDE a caller's transaction takes this
// rather than DB — see savingsContributionRepository.insert / archiveByGoal, which the savings
// basis switch drives inside the goal repository's transaction (#1625).
type Querier interface {
	Exec(ctx context.Context, sql string, arguments ...interface{}) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...interface{}) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...interface{}) pgx.Row
}

// Transaction is a Querier that can be committed or rolled back. pgx.Tx satisfies it.
//
// It exists so a SERVICE can own a transaction boundary that spans two repositories without
// importing pgx: savingsGoalService.applyBasisSwitch begins one through
// SavingsGoalRepository.BeginTx and hands it to both the goal repository and the contribution
// repository, so persist-basis / archive / seed commit or roll back together (#1625). Keeping the
// interface this narrow also keeps its test double to five methods instead of pgx.Tx's twelve.
type Transaction interface {
	Querier
	Commit(ctx context.Context) error
	Rollback(ctx context.Context) error
}
