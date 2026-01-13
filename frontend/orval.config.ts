import { defineConfig } from 'orval';

export default defineConfig({
  api: {
    input: {
      target: '../api/openapi.yaml',
      // Disable spectral validation - already validated in openapi-validate CI job
      // Fixes Node.js 22 compatibility issue with @stoplight/spectral-core's Ajv version
      validation: false,
    },
    output: {
      mode: 'tags-split',
      target: 'src/api/generated/endpoints',
      schemas: 'src/api/generated/models',
      client: 'react-query',
      httpClient: 'axios',
      mock: true,
      clean: true,
      prettier: true,
      override: {
        mutator: {
          path: './src/api/client.ts',
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
