module.exports = function configureBabel(api) {
  api.cache(true)
  return {
    presets: ['babel-preset-expo'],
    // Jest-only: the `react-native` -> `react-native-css/components` import rewrite that makes
    // `className` resolve. Metro performs this rewrite in its own resolver
    // (`react-native-css/dist/module/metro/resolver.js`, gated by `globalClassNamePolyfill`), so
    // a top-level Babel entry would be redundant for the native build and would perturb a working
    // path. See `jest.setup.ts` for the other half of the jest wiring.
    env: {
      test: {
        presets: ['nativewind/babel'],
      },
    },
  }
}
