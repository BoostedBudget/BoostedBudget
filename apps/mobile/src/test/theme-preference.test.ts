import {
  THEME_STORAGE_KEY,
  parseThemePreference,
  readThemePreference,
  resolveTheme,
  themeStorageKey,
} from '../theme/preference'

describe('parseThemePreference', () => {
  it('defaults to system when nothing is stored', () => {
    expect(parseThemePreference(null)).toBe('system')
    expect(parseThemePreference(undefined)).toBe('system')
  })

  it('accepts each valid stored preference', () => {
    expect(parseThemePreference('light')).toBe('light')
    expect(parseThemePreference('dark')).toBe('dark')
    expect(parseThemePreference('system')).toBe('system')
  })

  it.each(['{"x":1}', 'purple', '', 'Dark', ' light '])(
    'falls back to system for a corrupt stored value: %s',
    (value) => {
      expect(parseThemePreference(value)).toBe('system')
    },
  )

  it('rejects non-string stored values', () => {
    expect(parseThemePreference(1)).toBe('system')
    expect(parseThemePreference({ theme: 'dark' })).toBe('system')
  })
})

describe('themeStorageKey', () => {
  it('uses the shared desktop key when no user is known', () => {
    expect(THEME_STORAGE_KEY).toBe('bb-theme')
    expect(themeStorageKey()).toBe('bb-theme')
  })

  it('reserves a user-scoped namespace for #1657', () => {
    expect(themeStorageKey('user_123')).toBe('bb-theme:user_123')
  })
})

describe('resolveTheme', () => {
  it('resolves system against the device scheme', () => {
    expect(resolveTheme('system', 'dark')).toBe('dark')
    expect(resolveTheme('system', 'light')).toBe('light')
  })

  it('honours an explicit preference over the device scheme', () => {
    expect(resolveTheme('light', 'dark')).toBe('light')
    expect(resolveTheme('dark', 'light')).toBe('dark')
  })

  it('treats an unavailable device scheme as light', () => {
    expect(resolveTheme('system', null)).toBe('light')
    expect(resolveTheme('system', undefined)).toBe('light')
  })
})

describe('readThemePreference', () => {
  it('reads and parses the persisted value', async () => {
    const storage = { getItem: jest.fn(async () => 'dark') }

    await expect(readThemePreference(storage)).resolves.toBe('dark')
    expect(storage.getItem).toHaveBeenCalledWith('bb-theme')
  })

  it('reads the user-scoped key when a user id is supplied', async () => {
    const storage = { getItem: jest.fn(async () => 'light') }

    await expect(readThemePreference(storage, 'user_123')).resolves.toBe('light')
    expect(storage.getItem).toHaveBeenCalledWith('bb-theme:user_123')
  })

  it('falls back to system when the storage read rejects', async () => {
    const storage = {
      getItem: jest.fn(async () => {
        throw new Error('storage unavailable')
      }),
    }

    await expect(readThemePreference(storage)).resolves.toBe('system')
  })

  it('falls back to system when the stored value is corrupt', async () => {
    const storage = { getItem: jest.fn(async () => '{"x":1}') }

    await expect(readThemePreference(storage)).resolves.toBe('system')
  })
})

describe('async storage web build', () => {
  it('reads and writes window.localStorage under the exact, unprefixed key', async () => {
    const store = new Map<string, string>()
    const localStorage = {
      getItem: jest.fn((key: string) => store.get(key) ?? null),
      setItem: jest.fn((key: string, value: string) => {
        store.set(key, value)
      }),
      removeItem: jest.fn((key: string) => {
        store.delete(key)
      }),
      clear: jest.fn(() => store.clear()),
      key: jest.fn((index: number) => Array.from(store.keys())[index] ?? null),
      get length() {
        return store.size
      },
    }
    ;(globalThis as { localStorage?: unknown }).localStorage = localStorage

    const webStorage = (
      jest.requireActual('@react-native-async-storage/async-storage/lib/commonjs/AsyncStorage.js') as {
        default: {
          getItem: (key: string) => Promise<string | null>
          setItem: (key: string, value: string) => Promise<void>
        }
      }
    ).default

    await webStorage.setItem(THEME_STORAGE_KEY, 'dark')
    expect(localStorage.setItem).toHaveBeenCalledWith('bb-theme', 'dark')
    expect(store.get('bb-theme')).toBe('dark')

    await expect(webStorage.getItem(THEME_STORAGE_KEY)).resolves.toBe('dark')
    expect(localStorage.getItem).toHaveBeenCalledWith('bb-theme')
  })
})