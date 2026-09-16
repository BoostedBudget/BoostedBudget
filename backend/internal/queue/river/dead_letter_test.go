package river

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/riverqueue/river/rivertype"
	"github.com/stretchr/testify/assert"
)

func TestBuildErrorHistory(t *testing.T) {
	now := time.Now()
	job := &rivertype.JobRow{
		ID:          1,
		Kind:        "test_job",
		Attempt:     3,
		MaxAttempts: 3,
		Errors: []rivertype.AttemptError{
			{Attempt: 1, Error: "first error", At: now.Add(-2 * time.Hour)},
			{Attempt: 2, Error: "second error", At: now.Add(-1 * time.Hour)},
		},
	}

	currentErr := &testError{msg: "third error"}
	errors := buildErrorHistory(job, currentErr)

	assert.Len(t, errors, 3)
	assert.Equal(t, 1, errors[0].Attempt)
	assert.Equal(t, "first error", errors[0].Message)
	assert.Equal(t, 2, errors[1].Attempt)
	assert.Equal(t, "second error", errors[1].Message)
	assert.Equal(t, 3, errors[2].Attempt)
	assert.Equal(t, "third error", errors[2].Message)
}

func TestPanicError(t *testing.T) {
	pe := &panicError{val: "panic value", trace: "stack trace here"}
	assert.Contains(t, pe.Error(), "panic:")
	assert.Contains(t, pe.Error(), "stack trace here")
}

func TestJobErrorMarshal(t *testing.T) {
	errors := []domain.JobError{
		{Attempt: 1, Message: "error 1", Timestamp: time.Now()},
		{Attempt: 2, Message: "error 2", Timestamp: time.Now()},
	}

	jsonBytes, err := json.Marshal(errors)
	assert.NoError(t, err)
	assert.Contains(t, string(jsonBytes), "error 1")
	assert.Contains(t, string(jsonBytes), "error 2")
}

type testError struct {
	msg string
}

func (e *testError) Error() string {
	return e.msg
}
