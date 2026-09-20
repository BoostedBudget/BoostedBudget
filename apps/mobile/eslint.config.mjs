import js from '@eslint/js'
import tseslint from '@typescript-eslint/eslint-plugin'
import tsparser from '@typescript-eslint/parser'
import reactHooks from 'eslint-plugin-react-hooks'

export default [
  { ignores: ['dist', 'coverage', 'node_modules', '.expo', '*.config.*'] },
  {
    files: ['**/*.{ts,tsx}'],
    languageOptions: {
      parser: tsparser,
      parserOptions: { ecmaVersion: 'latest', sourceType: 'module', ecmaFeatures: { jsx: true } },
      globals: {
        __DEV__: 'readonly',
        URL: 'readonly',
        React: 'readonly',
        require: 'readonly',
        process: 'readonly',
        jest: 'readonly',
        describe: 'readonly',
        it: 'readonly',
        expect: 'readonly',
      },
    },
    plugins: { '@typescript-eslint': tseslint, 'react-hooks': reactHooks },
    rules: {
      ...js.configs.recommended.rules,
      ...tseslint.configs.recommended.rules,
      ...reactHooks.configs.recommended.rules,
      '@typescript-eslint/no-require-imports': 'off',
    },
  },
  {
    // These paths do not run inside React Native. The Playwright spec executes in a real browser
    // (and its `page.evaluate` callbacks are browser source), and the static-file server runs as a
    // Bun process. The base block above declares only the React Native / jest globals, so without
    // this every `document`/`window`/`Response`/`console` reference is a spurious `no-undef`.
    files: ['e2e/**/*.ts', 'scripts/**/*.ts'],
    languageOptions: {
      globals: {
        window: 'readonly',
        document: 'readonly',
        console: 'readonly',
        performance: 'readonly',
        getComputedStyle: 'readonly',
        requestAnimationFrame: 'readonly',
        Element: 'readonly',
        HTMLElement: 'readonly',
        CSSAnimation: 'readonly',
        Request: 'readonly',
        Response: 'readonly',
        Blob: 'readonly',
      },
    },
  },
]
