import { defineConfig } from 'orval';

// ORVAL VERSION PIN: Locked to 8.5.3; DO NOT upgrade without resolving below.
//
// BUG (v8.13.0): `override.query.useQuery: true` now applies to ALL operations (not just GET).
//   - 8.5.3 behavior: useQuery:true only affects GET (correct per-verb hooks).
//   - 8.13+ behavior: useQuery:true applied to DELETE/PUT/PATCH/POST → wrong shape
//     (UseQueryResult instead of UseMutationResult; no .mutateAsync()).
//
// UPGRADE PATH:
//   1. Replace global useQuery:true with per-tag overrides (target GET operations only).
//   2. Audit consumers for .mutateAsync() calls (should remain intact post-upgrade).
//   3. Verify GET hooks generate as UseQueryResult.
//   4. Re-test with `make generate-api-client` and validate.
//   Tracking: unresolved as of 2026-06-01.

export default defineConfig({
  api: {
    input: {
      target: '../../api/openapi.yaml',
      // Disable spectral validation - already validated in openapi-validate CI job.
      // Fixes Node.js 22 compatibility issue with @stoplight/spectral-core's Ajv version.
      // @ts-expect-error -- `validation` is a valid runtime option in orval 8.5.3 but is
      // missing from the InputOptions type definition; suppress until orval types catch up.
      validation: false,
    },
    output: {
      mode: 'tags-split',
      target: 'src/endpoints',
      schemas: 'src/models',
      client: 'react-query',
      httpClient: 'axios',
      mock: true,
      clean: true,
      prettier: true,
      override: {
        mutator: {
          path: './src/client.ts',
          name: 'customInstance',
        },
        query: {
          useQuery: true,
          useSuspenseQuery: false,
          useInfinite: false,
          signal: true,
        },
        mock: {
          type: 'msw',
          delay: 500,
        },
      },
    },
  },
});
