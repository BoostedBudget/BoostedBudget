/**
 * Builds the inline pre-paint theme bootstrap for the Expo Router web shell.
 *
 * The script is emitted into `<head>` BEFORE any module preload/script so the
 * first paint already carries the resolved theme's CSS custom properties. It
 * reads the persisted preference (`bb-theme`, plus the `bb-theme:<userId>`
 * variant reserved for #1657), resolves it against the system scheme, and
 * writes the resolved theme's variables onto `document.documentElement.style`.
 *
 * The variable list is derived from the generated `@boostedbudget/design-tokens/native`
 * adapter passed in by the caller — it is never hand-written, so it cannot drift
 * from the token source.
 */

export interface ThemeBootstrapTokens {
  light: { color: Record<string, string>; contrast: Record<string, string> }
  dark: { color: Record<string, string>; contrast: Record<string, string> }
}

/**
 * The resolution rule the bootstrap script applies. It mirrors
 * `resolveTheme` in `./preference.ts`; `theme-bootstrap.test.ts` asserts the two
 * agree for all six (preference × system) combinations.
 */
export function resolveBootstrapTheme(
  preference: string,
  systemScheme: string | null | undefined,
): 'light' | 'dark' {
  if (preference === 'system') {
    return systemScheme === 'dark' ? 'dark' : 'light'
  }
  return preference === 'dark' ? 'dark' : 'light'
}

/**
 * Serialises the resolution rule into a lookup table so the emitted script
 * cannot disagree with `resolveBootstrapTheme` (and therefore `preference.ts`):
 * the table IS the function's output for every combination.
 */
function buildResolutionTable(): Record<string, Record<string, 'light' | 'dark'>> {
  const preferences = ['light', 'dark', 'system'] as const
  const schemes = ['light', 'dark'] as const

  return Object.fromEntries(
    preferences.map((preference) => [
      preference,
      Object.fromEntries(
        schemes.map((scheme) => [scheme, resolveBootstrapTheme(preference, scheme)]),
      ),
    ]),
  )
}

export function buildThemeBootstrapScript(tokens: ThemeBootstrapTokens): string {
  const themes = {
    light: { ...tokens.light.color, ...tokens.light.contrast },
    dark: { ...tokens.dark.color, ...tokens.dark.contrast },
  }
  const resolution = buildResolutionTable()

  return `(function () {
  var THEME_KEY = 'bb-theme';
  var THEME_SCOPED_PREFIX = 'bb-theme:';
  var THEMES = ${JSON.stringify(themes)};
  var RESOLUTION = ${JSON.stringify(resolution)};
  function readPreference() {
    var value = null;
    try {
      value = window.localStorage.getItem(THEME_KEY);
      for (var i = 0; i < window.localStorage.length; i += 1) {
        var key = window.localStorage.key(i);
        if (key && key.indexOf(THEME_SCOPED_PREFIX) === 0) {
          var scoped = window.localStorage.getItem(key);
          if (scoped) value = scoped;
        }
      }
    } catch (error) {
      value = null;
    }
    return value === 'light' || value === 'dark' || value === 'system' ? value : 'system';
  }
  var systemScheme = 'light';
  try {
    systemScheme = window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
  } catch (error) {
    systemScheme = 'light';
  }
  var resolved = RESOLUTION[readPreference()][systemScheme];
  var variables = THEMES[resolved];
  var root = document.documentElement;
  for (var name in variables) {
    if (Object.prototype.hasOwnProperty.call(variables, name)) {
      root.style.setProperty('--' + name, variables[name]);
    }
  }
  root.setAttribute('data-bb-theme', resolved);
})();`
}