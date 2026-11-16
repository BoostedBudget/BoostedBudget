const jestExpoPreset = require('jest-expo/jest-preset')

module.exports = {
  preset: 'jest-expo',
  transform: {
    ...jestExpoPreset.transform,
    '^.+\\.mjs$': jestExpoPreset.transform['\\.[jt]sx?$'],
  },
  setupFilesAfterEnv: ['<rootDir>/jest.setup.ts'],
  moduleNameMapper: {
    '^@babel/runtime/(.*)$': '<rootDir>/../../node_modules/@babel/runtime/$1',
    '\\.(css)$': '<rootDir>/src/test/style-mock.js',
    // jest-resolve does not expand the package's subpath-pattern export
    // (`"./endpoints/*/*"` in packages/api-client/package.json), so the generated
    // `@boostedbudget/api-client/endpoints/<feature>/<feature>` specifier fails to resolve here
    // even though Node's resolver accepts it. The literal `./client` and `./models` keys resolve
    // natively and are deliberately left to the exports map. This is only needed by the
    // contract-integration tier (todo 21), which imports real generated functions.
    '^@boostedbudget/api-client/endpoints/(.*)$':
      '<rootDir>/../../packages/api-client/src/endpoints/$1',
  },
  collectCoverageFrom: [
    'app/**/*.{ts,tsx}',
    'src/**/*.{ts,tsx}',
    '!**/*.d.ts',
    '!src/test/**',
  ],
  coverageProvider: 'v8',
  coverageThreshold: {
    global: {
      branches: 82,
      functions: 82,
      lines: 82,
      statements: 82,
    },
  },
  // Bun's isolated linker nests packages under node_modules/.bun before their
  // conventional node_modules path. Keep both layers transformable by Expo's
  // Babel preset while retaining Jest's normal dependency exclusion.
  transformIgnorePatterns: [
    '/node_modules/(?!\\.bun/|.*node_modules/magic-string|react-native|@react-native|expo|@expo|@clerk|expo-router|@tanstack|nanoid|standard-navigation|decode-uri-component|query-string|split-on-first|filter-obj|magic-string)',
  ],
  // `/e2e/` holds the Playwright visual/accessibility suite (Todo 22). `jest-expo` sets no
  // `testMatch`, so jest's default `**/?(*.)+(spec|test).[jt]s?(x)` would discover
  // `e2e/visual/gallery.spec.ts` and hard-fail on `@playwright/test` ("did not expect test() to
  // be called here"), breaking the whole mobile suite and the `mobile-test` CI gate.
  testPathIgnorePatterns: ['/node_modules/', '/dist/', '/e2e/'],
}
