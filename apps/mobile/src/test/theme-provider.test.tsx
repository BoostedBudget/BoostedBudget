import AsyncStorage from '@react-native-async-storage/async-storage'
import { beforeEach } from '@jest/globals'
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react-native'
import { Text } from 'react-native'
import { ThemeProvider, useTheme } from '../theme/ThemeProvider'

function ThemeProbe() {
  const { preference, resolvedTheme, setPreference, hydrated } = useTheme()

  return (
    <>
      <Text testID="preference">{preference}</Text>
      <Text testID="resolved">{resolvedTheme}</Text>
      <Text testID="hydrated">{String(hydrated)}</Text>
      <Text testID="set-dark" onPress={() => setPreference('dark')}>
        set dark
      </Text>
      <Text testID="set-light" onPress={() => setPreference('light')}>
        set light
      </Text>
    </>
  )
}

describe('ThemeProvider', () => {
  beforeEach(async () => {
    await AsyncStorage.clear()
    jest.clearAllMocks()
  })

  it('hydrates from the persisted preference and exposes hydrated', async () => {
    await AsyncStorage.setItem('bb-theme', 'dark')

    render(
      <ThemeProvider>
        <ThemeProbe />
      </ThemeProvider>,
    )

    await waitFor(() => expect(screen.getByTestId('hydrated')).toHaveTextContent('true'))
    expect(screen.getByTestId('preference')).toHaveTextContent('dark')
    expect(screen.getByTestId('resolved')).toHaveTextContent('dark')
  })

  it('defaults to system when nothing is persisted', async () => {
    render(
      <ThemeProvider>
        <ThemeProbe />
      </ThemeProvider>,
    )

    await waitFor(() => expect(screen.getByTestId('hydrated')).toHaveTextContent('true'))
    expect(screen.getByTestId('preference')).toHaveTextContent('system')
  })

  it('persists the preference on setPreference', async () => {
    render(
      <ThemeProvider>
        <ThemeProbe />
      </ThemeProvider>,
    )
    await waitFor(() => expect(screen.getByTestId('hydrated')).toHaveTextContent('true'))

    await act(async () => {
      fireEvent.press(screen.getByTestId('set-dark'))
    })

    expect(screen.getByTestId('preference')).toHaveTextContent('dark')
    await waitFor(() => expect(AsyncStorage.setItem).toHaveBeenCalledWith('bb-theme', 'dark'))
    await expect(AsyncStorage.getItem('bb-theme')).resolves.toBe('dark')
  })

  it('falls back to system and still hydrates when the storage read rejects', async () => {
    ;(AsyncStorage.getItem as jest.Mock).mockRejectedValueOnce(new Error('storage unavailable'))

    render(
      <ThemeProvider>
        <ThemeProbe />
      </ThemeProvider>,
    )

    await waitFor(() => expect(screen.getByTestId('hydrated')).toHaveTextContent('true'))
    expect(screen.getByTestId('preference')).toHaveTextContent('system')
  })

  it('never renders the raw stored value', async () => {
    await AsyncStorage.setItem('bb-theme', '{"x":1}')

    render(
      <ThemeProvider>
        <ThemeProbe />
      </ThemeProvider>,
    )

    await waitFor(() => expect(screen.getByTestId('hydrated')).toHaveTextContent('true'))
    expect(screen.queryByText('{"x":1}')).not.toBeOnTheScreen()
    expect(screen.getByTestId('preference')).toHaveTextContent('system')
  })

  it('refuses to read theme state outside a provider', () => {
    const consoleError = jest.spyOn(globalThis.console, 'error').mockImplementation(() => undefined)

    expect(() => render(<ThemeProbe />)).toThrow('useTheme must be used within a ThemeProvider')

    consoleError.mockRestore()
  })
})