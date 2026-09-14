import { defineConfig } from 'orval';

// orval generates the correct per-verb React Query hooks from its defaults:
//   - GET     → useQuery    (UseQueryResult)
//   - non-GET → useMutation (UseMutationResult, with .mutateAsync())
// Do NOT set a global `override.query.useQuery: true`. Since orval 8.13 that flag applies
// to ALL operations (POST/PUT/PATCH/DELETE), emitting UseQueryResult and breaking every
// `.mutateAsync()` callsite. See issue #1252 / PR #1251.

export default defineConfig({
  api: {
    input: {
      target: '../../api/openapi.yaml',
      // Skip spectral validation here — the spec is validated in the openapi-validate CI
      // job, and spectral's bundled Ajv has a Node.js 22 incompatibility.
      unsafeDisableValidation: true,
    },
    output: {
      mode: 'tags-split',
      target: 'src/endpoints',
      schemas: 'src/models',
      client: 'react-query',
      httpClient: 'axios',
      // Preserve declared request headers in generated endpoint signatures. This makes
      // required internal credentials impossible to omit from the typed client contract.
      headers: true,
      // MSW-only mocks. In orval v8 `mock: true` emits BOTH msw and faker outputs, so we
      // pin the generator list to msw to keep the generated tree stable.
      mock: {
        generators: [{ type: 'msw', delay: 500 }],
      },
      clean: true,
      prettier: true,
      override: {
        mutator: {
          path: './src/client.ts',
          name: 'customInstance',
        },
        query: {
          useSuspenseQuery: false,
          useInfinite: false,
          signal: true,
        },
      },
    },
  },
});
