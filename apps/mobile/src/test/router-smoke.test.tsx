jest.unmock('expo-router')

import { afterEach } from '@jest/globals'
import { cleanup, fireEvent, renderRouter, screen } from 'expo-router/testing-library'

afterEach(() => {
  cleanup()
  jest.useRealTimers()
})

describe('Expo Router smoke coverage', () => {
  it('renders the home route and follows the readiness link', () => {
    const router = renderRouter('./app')

    expect(screen.getByRole('header', { name: 'BoostedBudget' })).toBeOnTheScreen()
    fireEvent.press(screen.getByRole('link', { name: 'View app readiness' }))

    expect(router.getPathname()).toBe('/readiness')
    expect(screen.getByRole('header', { name: 'App readiness' })).toBeOnTheScreen()
  })

  it('renders the readiness route directly', () => {
    const router = renderRouter('./app', { initialUrl: '/readiness' })

    expect(router.getPathname()).toBe('/readiness')
    expect(screen.getByRole('header', { name: 'App readiness' })).toBeOnTheScreen()
  })
})
