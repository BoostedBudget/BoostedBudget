package cache

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/maxnussbaum/BoostedBudget/internal/redisclient"
)

type typedFixture struct {
	Total int    `json:"total"`
	Label string `json:"label"`
}

func TestGetOrLoad_Typed_ColdMissThenWarmHit(t *testing.T) {
	fake := redisclient.NewFake("cache")
	c := NewCache(Options{Client: fake})
	ctx := context.Background()
	k := testKey("TypedMethod")

	var calls int
	load := func(ctx context.Context) (typedFixture, error) {
		calls++
		return typedFixture{Total: 42, Label: "first"}, nil
	}

	got, err := GetOrLoad(ctx, c, k, load)
	require.NoError(t, err)
	assert.Equal(t, typedFixture{Total: 42, Label: "first"}, got)
	assert.Equal(t, 1, calls)

	load2 := func(ctx context.Context) (typedFixture, error) {
		calls++
		return typedFixture{Total: 999, Label: "should not be seen"}, nil
	}
	got2, err := GetOrLoad(ctx, c, k, load2)
	require.NoError(t, err)
	assert.Equal(t, typedFixture{Total: 42, Label: "first"}, got2, "warm hit must return the cached value, not re-invoke the loader")
	assert.Equal(t, 1, calls)
}

func TestGetOrLoad_Typed_LoaderErrorPropagates(t *testing.T) {
	fake := redisclient.NewFake("cache")
	c := NewCache(Options{Client: fake})
	ctx := context.Background()
	boom := errors.New("boom")

	_, err := GetOrLoad(ctx, c, testKey("TypedMethod"), func(ctx context.Context) (typedFixture, error) {
		return typedFixture{}, boom
	})
	assert.ErrorIs(t, err, boom)
}
