package operations

import (
	"reflect"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/maxnussbaum/BoostedBudget/internal/services"
	"github.com/stretchr/testify/require"
)

func TestRevenueCatSchemasAcceptNullObjectObservations(t *testing.T) {
	registry := huma.NewMapRegistry("#/components/schemas/", huma.DefaultSchemaNamer)
	summary := registry.Schema(reflect.TypeOf(services.RevenueCatSummary{}), false, "RevenueCatSummary")
	body := registry.Schema(reflect.TypeOf(AdminRevenueCatUserOutput{}.Body), false, "AdminRevenueCatUserOutputBody")
	health := registry.Schema(reflect.TypeOf(AdminRevenueCatHealthOutput{}.Body), false, "AdminRevenueCatHealthOutputBody")
	for _, entry := range []struct {
		schema *huma.Schema
		field  string
	}{
		{summary, "effective"}, {summary, "own"}, {summary, "paid"}, {summary, "comp"}, {summary, "trial"}, {body, "state"},
		{health, "metrics"},
	} {
		t.Run(entry.field, func(t *testing.T) {
			require.Contains(t, entry.schema.Required, entry.field)
			property := entry.schema.Properties[entry.field]
			require.NotNil(t, property)
			result := &huma.ValidateResult{}
			huma.Validate(registry, property, &huma.PathBuffer{}, huma.ModeReadFromServer, nil, result)
			require.Empty(t, result.Errors, "a missing observation is emitted as JSON null")
			result = &huma.ValidateResult{}
			huma.Validate(registry, property, &huma.PathBuffer{}, huma.ModeReadFromServer, "invalid object", result)
			require.NotEmpty(t, result.Errors, "nullable object must retain its object contract")
		})
	}
}
