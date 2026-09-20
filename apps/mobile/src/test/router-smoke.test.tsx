jest.unmock('expo-router')

import { afterEach } from '@jest/globals'
import { act } from '@testing-library/react-native'
import { cleanup, fireEvent, renderRouter, screen } from 'expo-router/testing-library'

afterEach(() => {
  cleanup()
  jest.useRealTimers()
})

describe('Expo Router smoke coverage', () => {
  it('renders the home route and follows the readiness link', async () => {
    const router = renderRouter('./app')
    await act(async () => {})

    expect(screen.getByRole('header', { name: 'BoostedBudget' })).toBeOnTheScreen()
    fireEvent.press(screen.getByRole('link', { name: 'View app readiness' }))

    expect(router.getPathname()).toBe('/readiness')
    expect(screen.getByRole('header', { name: 'App readiness' })).toBeOnTheScreen()
  })

  it('renders the readiness route directly', async () => {
    const router = renderRouter('./app', { initialUrl: '/readiness' })
    await act(async () => {})

    expect(router.getPathname()).toBe('/readiness')
    expect(screen.getByRole('header', { name: 'App readiness' })).toBeOnTheScreen()
  })
})
