import { fireEvent, render, screen } from '@testing-library/react-native'
import RootLayout from '../../app/_layout'
import HomeScreen from '../../app/index'
import ReadinessScreen from '../../app/readiness'

describe('universal app routes', () => {
  it('mounts the root provider and route stack', () => {
    render(<RootLayout />)
    expect(screen.getByText('index')).toBeOnTheScreen()
    expect(screen.getByText('readiness')).toBeOnTheScreen()
  })

  it('renders an accessible foundation-only home surface', () => {
    render(<HomeScreen />)
    expect(screen.getByRole('header', { name: 'BoostedBudget' })).toBeOnTheScreen()
    expect(screen.getByRole('link', { name: 'View app readiness' })).toBeOnTheScreen()
  })

  it('renders only safe configuration status on the readiness route', () => {
    render(<ReadinessScreen environment={{ EXPO_PUBLIC_API_URL: 'https://api.example.com' }} platform="ios" />)
    expect(screen.getByRole('header', { name: 'App readiness' })).toBeOnTheScreen()
    expect(screen.getByText('API URL: configured and valid')).toBeOnTheScreen()
    expect(screen.getByText('Billing adapter: RevenueCat (contract only)')).toBeOnTheScreen()
    expect(screen.queryByText('https://api.example.com')).not.toBeOnTheScreen()
  })

  it('exposes readiness details as accessible status content', () => {
    render(<ReadinessScreen environment={{}} platform="web" />)
    fireEvent.press(screen.getByRole('button', { name: 'Check configuration' }))
    const liveStatus = screen
      .getAllByText('API URL: missing or invalid')
      .find((element) => element.props.accessibilityLiveRegion === 'polite')
    expect(liveStatus).toBeDefined()
  })
})
