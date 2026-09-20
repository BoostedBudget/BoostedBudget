import { afterEach } from '@jest/globals'
import { render, screen } from '@testing-library/react-native'
import { View } from 'react-native'
import { Alert, type AlertVariant } from '../../components/ui/Alert'
import { FormField } from '../../components/ui/FormField'
import { TextField } from '../../components/ui/TextField'
import { Toast, type ToastVariant } from '../../components/ui/Toast'

afterEach(() => {
  jest.restoreAllMocks()
})

const ALERT_VARIANTS: readonly AlertVariant[] = ['info', 'success', 'warning', 'danger']
const TOAST_VARIANTS: readonly ToastVariant[] = ['info', 'success', 'warning', 'danger']

/**
 * Criterion 8 (#1656): error announcements. A field error is announced politely (it waits for a
 * pause); an `Alert` interrupts assertively; a `Toast` is assertive only for `danger`, polite
 * otherwise, so a routine confirmation never interrupts.
 */
describe('accessibility: error announcements (criterion 8)', () => {
  it('announces a FormField error in a polite live region', () => {
    render(
      <FormField label="Email" error="Email is required">
        <TextField testID="ann-field" />
      </FormField>,
    )

    const error = screen.getByText('Email is required')
    expect(error.props.accessibilityLiveRegion).toBe('polite')
    expect(screen.getByTestId('ann-field').props.accessibilityState.invalid).toBe(true)
  })

  it('gives every Alert the alert role and an assertive live region', () => {
    render(
      <View>
        {ALERT_VARIANTS.map((variant) => (
          <Alert key={variant} variant={variant} testID={`ann-alert-${variant}`}>
            body
          </Alert>
        ))}
      </View>,
    )

    for (const variant of ALERT_VARIANTS) {
      const node = screen.getByTestId(`ann-alert-${variant}`)
      expect(node.props.accessibilityRole).toBe('alert')
      expect(node.props.accessibilityLiveRegion).toBe('assertive')
    }
  })

  it('gives every Toast the alert role with a variant-appropriate live region', () => {
    render(
      <View>
        {TOAST_VARIANTS.map((variant) => (
          <Toast key={variant} variant={variant} message="Saved" testID={`ann-toast-${variant}`} />
        ))}
      </View>,
    )

    for (const variant of TOAST_VARIANTS) {
      const node = screen.getByTestId(`ann-toast-${variant}`)
      expect(node.props.accessibilityRole).toBe('alert')
      expect(node.props.accessibilityLiveRegion).toBe(variant === 'danger' ? 'assertive' : 'polite')
    }
  })

  it('keeps the error in the accessibility tree, not only in style', () => {
    render(
      <FormField label="Email" error="Invalid address">
        <TextField testID="ann-field-2" />
      </FormField>,
    )

    expect(screen.getByText('Invalid address')).toBeTruthy()
  })
})
