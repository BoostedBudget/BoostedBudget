import { act, fireEvent, render, screen } from '@testing-library/react-native'
import { useQueryClient } from '@tanstack/react-query'
import { ThemeProvider } from 'expo-router'
import { StatusBar } from 'expo-status-bar'
import { Text } from 'react-native'
import RootLayout, { MobileAppProviders } from '../../app/_layout'
import HomeScreen from '../../app/index'
import ReadinessScreen from '../../app/readiness'
import { ThemeProvider as ThemePreferenceProvider } from '../theme/ThemeProvider'

describe('universal app routes', () => {
  it('wires the system appearance into navigation chrome and the status bar', async () => {
    render(<RootLayout />)
    await act(async () => {})

    expect(ThemeProvider).toHaveBeenCalledWith(
      expect.objectContaining({ value: expect.objectContaining({ dark: false }) }),
      undefined,
    )
    expect(StatusBar).toHaveBeenCalledWith(expect.objectContaining({ style: 'dark' }), undefined)
  })

  it('provides a real TanStack Query client to route content', async () => {
    function QueryClientProbe() {
      const client = useQueryClient()
      return <Text>{String(client.getDefaultOptions().queries?.staleTime)}</Text>
    }

    render(
      <MobileAppProviders>
        <QueryClientProbe />
      </MobileAppProviders>,
    )
    await act(async () => {})
    expect(screen.getByText('30000')).toBeOnTheScreen()
  })

  it('renders an accessible foundation-only home surface', async () => {
    render(
      <ThemePreferenceProvider>
        <HomeScreen />
      </ThemePreferenceProvider>,
    )
    await act(async () => {})
    expect(screen.getByRole('header', { name: 'BoostedBudget' })).toBeOnTheScreen()
    expect(screen.getByRole('link', { name: 'View app readiness' })).toBeOnTheScreen()
  })

  it('renders only safe configuration status on the readiness route', async () => {
    render(
      <ThemePreferenceProvider>
        <ReadinessScreen environment={{ EXPO_PUBLIC_API_URL: 'https://api.example.com' }} platform="ios" />
      </ThemePreferenceProvider>,
    )
    await act(async () => {})
    expect(screen.getByRole('header', { name: 'App readiness' })).toBeOnTheScreen()
    expect(screen.getByText('API URL: configured and valid')).toBeOnTheScreen()
    expect(screen.getByText('Billing adapter: RevenueCat (contract only)')).toBeOnTheScreen()
    expect(screen.queryByText('https://api.example.com')).not.toBeOnTheScreen()
  })

  it('exposes readiness details as accessible status content', async () => {
    render(
      <ThemePreferenceProvider>
        <ReadinessScreen environment={{}} platform="web" />
      </ThemePreferenceProvider>,
    )
    await act(async () => {})
    fireEvent.press(screen.getByRole('button', { name: 'Check configuration' }))
    const liveStatus = screen
      .getAllByText('API URL: missing or invalid')
      .find((element) => element.props.accessibilityLiveRegion === 'polite')
    expect(liveStatus).toBeDefined()
  })

  it('renders an explicit fallback instead of crashing on an unsupported platform', async () => {
    render(
      <ThemePreferenceProvider>
        <ReadinessScreen environment={{}} platform="windows" />
      </ThemePreferenceProvider>,
    )
    await act(async () => {})
    expect(screen.getByText('Billing adapter: unavailable on this platform')).toBeOnTheScreen()
  })
})
