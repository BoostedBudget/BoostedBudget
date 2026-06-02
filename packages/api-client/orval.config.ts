import { defineConfig } from 'orval';

// ORVAL VERSION PIN: orval is pinned to 8.5.3 in package.json and MUST NOT be bumped
// past 8.5.x without resolving the following breaking change first.
//
// PROBLEM (tested 2026-06-01 against v8.13.0):
//   The global `override.query.useQuery: true` setting in this config causes orval 8.13+
//   to generate ALL operations (including DELETE/PUT/PATCH/POST) as UseQueryResult hooks
//   backed by `useQuery()`. This is query-shaped output — hooks auto-run on mount and
//   have no `.mutateAsync()` — which is the wrong primitive for mutation operations.
//   In 8.5.3, `useQuery: true` only applies to GET endpoints; non-GET operations
//   correctly produce `UseMutationResult` + `useMutation()`.
//
// ROOT CAUSE:
//   Orval 8.13+ changed `override.query.useQuery: true` to mean "apply to ALL operations"
//   rather than "apply to GETs only". Per the docs: "Explicitly setting `useQuery: true`
//   applies it to all operations." The interaction with `useMutation` (the override knob)
//   doesn't restore the desired per-verb behavior: when both flags are set, `useQuery`
//   takes precedence for non-GET operations, so DELETE/PUT/PATCH still end up query-shaped.
//
// TO UPGRADE:
//   1. Remove the global `override.query.useQuery: true` (or replace with per-tag overrides).
//   2. Audit all consumers of the generated API for `.mutateAsync()` calls — they should
//      remain intact since non-GET operations would default back to mutation-shaped hooks.
//   3. Confirm GET-only hooks (e.g. useGetSavingsGoals) still generate as UseQueryResult.
//   4. Re-run `make generate-api-client` with the new orval version and verify shapes.
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
