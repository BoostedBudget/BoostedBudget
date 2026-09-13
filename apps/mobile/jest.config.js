module.exports = {
  preset: 'jest-expo',
  setupFilesAfterEnv: ['<rootDir>/jest.setup.ts'],
  moduleNameMapper: {
    '^@babel/runtime/(.*)$': '<rootDir>/../../node_modules/@babel/runtime/$1',
    '\\.(css)$': '<rootDir>/src/test/style-mock.js',
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
    '/node_modules/(?!\\.bun/|react-native|@react-native|expo|@expo|expo-router|@tanstack)',
  ],
  testPathIgnorePatterns: ['/node_modules/', '/dist/'],
}
