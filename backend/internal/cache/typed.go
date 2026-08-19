package cache

import (
	"context"
	"encoding/json"
)

// GetOrLoad is generic typed sugar over Cache.GetOrLoad: it JSON-marshals load's result before
// handing it to the byte-oriented cache, and unmarshals whatever comes back (cached or freshly
// loaded) into T. Go methods cannot have type parameters, so this must be a package-level function
// rather than a method on the Cache interface.
func GetOrLoad[T any](
	ctx context.Context, c Cache, k Key, load func(context.Context) (T, error),
) (T, error) {
	var zero T

	raw, _, err := c.GetOrLoad(ctx, k, func(ctx context.Context) ([]byte, error) {
		v, lerr := load(ctx)
		if lerr != nil {
			return nil, lerr
		}
		return json.Marshal(v)
	})
	if err != nil {
		return zero, err
	}

	var out T
	if uerr := json.Unmarshal(raw, &out); uerr != nil {
		// Should be structurally unreachable — decodeEnvelope already validated the schema
		// version before returning these bytes — but fail closed by re-running the loader
		// directly rather than returning a zero value that looks like a legitimate result.
		return load(ctx)
	}
	return out, nil
}
