package river

import (
	"errors"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

// pgErrorContext returns everything Postgres sent, if a *pgconn.PgError is anywhere in err's
// chain. It uses errors.As deliberately: the flattened message a string match would have to
// parse is exactly the lossy artefact this exists to replace, and a wrapped PgError's text is
// already truncated to Severity/Message/SQLSTATE by pgconn — Detail never appears in it at all.
func pgErrorContext(err error) *domain.PgErrorContext {
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr == nil {
		return nil
	}
	return &domain.PgErrorContext{
		Code:           pgErr.Code,
		Severity:       pgErr.Severity,
		Message:        pgErr.Message,
		Detail:         pgErr.Detail,
		Hint:           pgErr.Hint,
		ConstraintName: pgErr.ConstraintName,
		TableName:      pgErr.TableName,
		SchemaName:     pgErr.SchemaName,
		ColumnName:     pgErr.ColumnName,
		Routine:        pgErr.Routine,
	}
}

// errorChain flattens err's wrapping into an ordered list, outermost boundary first.
//
// Each entry carries only the layer's OWN contribution: the wrapped error's text is trimmed off
// the tail, so `fmt.Errorf("investment sync failed: %w", inner)` contributes
// "investment sync failed" rather than repeating everything beneath it. The innermost error
// contributes its full text. A layer that added nothing of its own is skipped.
func errorChain(err error) []string {
	var out []string
	appendChain(err, &out)
	return out
}

func appendChain(err error, out *[]string) {
	for err != nil {
		msg := strings.TrimSpace(err.Error())

		switch unwrapped := err.(type) {
		case interface{ Unwrap() []error }:
			// errors.Join and friends: no single tail to trim, so record this layer verbatim
			// and then record every branch.
			if msg != "" {
				*out = append(*out, msg)
			}
			for _, branch := range unwrapped.Unwrap() {
				appendChain(branch, out)
			}
			return
		}

		inner := errors.Unwrap(err)
		if inner != nil {
			msg = trimWrapped(msg, inner.Error())
		}
		if msg != "" {
			*out = append(*out, msg)
		}
		err = inner
	}
}

// trimWrapped removes the wrapped error's own text (and the separator that joined it) from the
// tail of a wrapping error's message. It returns outer unchanged when the tail does not match,
// which is the case for a custom Error() that reorders or reformats what it wraps.
func trimWrapped(outer, inner string) string {
	trimmed := strings.TrimSpace(strings.TrimSuffix(outer, strings.TrimSpace(inner)))
	if trimmed == outer {
		return outer
	}
	return strings.TrimSpace(strings.TrimSuffix(trimmed, ":"))
}
