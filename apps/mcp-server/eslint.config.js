import js from '@eslint/js'
import globals from 'globals'
import tseslint from '@typescript-eslint/eslint-plugin'
import tsparser from '@typescript-eslint/parser'

export default [
  {
    ignores: [
      'dist',
      'node_modules',
      'src/spec/generated/**',
      'src/client/generated/**',
    ],
  },
  // Cloudflare Worker source files
  {
    files: ['src/**/*.ts'],
    languageOptions: {
      ecmaVersion: 2022,
      globals: {
        ...globals.browser, // Workers have browser-like globals (fetch, URL, Request, etc.)
        crypto: 'readonly',
        console: 'readonly',
        ExecutionContext: 'readonly',
      },
      parser: tsparser,
      parserOptions: {
        ecmaVersion: 'latest',
        sourceType: 'module',
      },
    },
    plugins: {
      '@typescript-eslint': tseslint,
    },
    rules: {
      ...js.configs.recommended.rules,
      ...tseslint.configs.recommended.rules,
      // TypeScript's compiler handles undefined variable checks — no-undef produces
      // false positives for TypeScript types (RequestInit, etc.) in Worker code.
      'no-undef': 'off',
      '@typescript-eslint/no-unused-vars': [
        'error',
        {
          argsIgnorePattern: '^_',
          varsIgnorePattern: '^_',
          destructuredArrayIgnorePattern: '^_',
          caughtErrorsIgnorePattern: '^_',
        },
      ],
      '@typescript-eslint/no-explicit-any': 'warn',
      // Allow casting to unknown first (common in Workers edge code)
      '@typescript-eslint/no-unsafe-assignment': 'off',
      // Empty interfaces are used for extension points in agent types
      '@typescript-eslint/no-empty-object-type': 'off',
    },
  },
  // Build scripts — Node.js runtime
  {
    files: ['scripts/**/*.ts'],
    languageOptions: {
      ecmaVersion: 2022,
      globals: {
        ...globals.node,
      },
      parser: tsparser,
      parserOptions: {
        ecmaVersion: 'latest',
        sourceType: 'module',
      },
    },
    plugins: {
      '@typescript-eslint': tseslint,
    },
    rules: {
      ...js.configs.recommended.rules,
      ...tseslint.configs.recommended.rules,
      'no-undef': 'off',
      '@typescript-eslint/no-explicit-any': 'off', // Allow any in build/tooling scripts
      '@typescript-eslint/no-unused-vars': [
        'error',
        {
          argsIgnorePattern: '^_',
          varsIgnorePattern: '^_',
        },
      ],
    },
  },
]
