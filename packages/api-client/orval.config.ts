import { defineConfig, defineTransformer } from 'orval';

const requestMethods = ['get', 'put', 'post', 'delete', 'options', 'head', 'patch'] as const;

// Orval's request-header generation switch is global, not operation-specific. Keep the published
// OpenAPI complete, but filter headers from the generator's private view for every operation except
// the Worker-only telemetry endpoint that must expose its required credential in the typed client.
const preserveTelemetryRequestHeaders = defineTransformer((spec) => {
  for (const pathItem of Object.values(spec.paths ?? {})) {
    if ('$ref' in pathItem) continue;
    for (const method of requestMethods) {
      const operation = pathItem[method];
      if (!operation || operation.operationId === 'recordMCPToolTelemetry') continue;
      operation.parameters = operation.parameters?.filter(
        parameter => '$ref' in parameter || parameter.in !== 'header',
      );
    }
  }
  return spec;
});

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
      override: {
        transformer: preserveTelemetryRequestHeaders,
      },
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
      // Header generation is global in Orval; the input transformer above scopes it to telemetry.
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
