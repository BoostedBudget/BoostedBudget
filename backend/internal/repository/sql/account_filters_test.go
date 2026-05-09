package accountsql_test

import (
	"strings"
	"testing"

	accountsql "github.com/maxnussbaum/BoostedBudget/internal/repository/sql"
	"github.com/stretchr/testify/assert"
)

func TestChainExpansionWhere_ContainsParamAndColumn(t *testing.T) {
	result := accountsql.ChainExpansionWhere("account_id", 1)
	assert.Contains(t, result, "account_id = $1")
	assert.Contains(t, result, "account_id IN (")
	assert.Contains(t, result, "user_account_id")
}

func TestChainExpansionWhere_CustomParamIndex(t *testing.T) {
	result := accountsql.ChainExpansionWhere("account_id", 3)
	assert.Contains(t, result, "$3")
	assert.NotContains(t, result, "$1")
}

func TestChainExpansionWhere_CustomColumn(t *testing.T) {
	result := accountsql.ChainExpansionWhere("l.account_id", 2)
	assert.True(t, strings.Contains(result, "l.account_id = $2"))
}
