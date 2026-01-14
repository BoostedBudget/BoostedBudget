/**
 * Test utilities for fast form interactions
 *
 * Issue #XXX: Frontend Test Performance Improvement
 *
 * These utilities optimize test performance by:
 * 1. Using userEvent.setup({ delay: null }) to remove keystroke delays
 * 2. Using fireEvent.change() for simple form filling instead of userEvent.type()
 *
 * Use these helpers to reduce test timeouts from 60-120s to under 15s.
 *
 * WHEN TO USE:
 * - Form submission tests
 * - Validation tests
 * - API call tests
 * - State after form completion
 *
 * WHEN TO KEEP userEvent.type():
 * - Filter/search tests with debounce behavior
 * - Real-time validation as typing
 * - Character limit/masking tests
 * - Keyboard navigation tests
 */

import { fireEvent, screen, waitFor } from '@testing-library/react'
import userEvent, { type UserEvent } from '@testing-library/user-event'

/**
 * Create a userEvent instance with no keystroke delays.
 * Use for tests where typing speed doesn't affect the behavior being tested.
 *
 * @example
 * const user = createFastUser()
 * await user.click(button)
 * await user.type(input, 'text') // Still uses type but without delays
 */
export function createFastUser(): UserEvent {
  return userEvent.setup({ delay: null })
}

/**
 * Fast form field filling using fireEvent.change().
 * ~90% faster than userEvent.type() for simple value setting.
 *
 * @example
 * fillInput(screen.getByLabelText(/name/i), 'Test Budget')
 */
export function fillInput(element: HTMLElement, value: string | number): void {
  fireEvent.change(element, { target: { value: String(value) } })
}

/**
 * Fill input by label text - convenience wrapper.
 *
 * @example
 * fillInputByLabel(/rule name/i, 'My Rule')
 * fillInputByLabel(/amount/i, '2000')
 */
export function fillInputByLabel(
  labelText: string | RegExp,
  value: string | number
): void {
  const input = screen.getByLabelText(labelText)
  fillInput(input, value)
}

/**
 * Fill input by placeholder text - convenience wrapper.
 *
 * @example
 * fillInputByPlaceholder(/enter value/i, 'test value')
 */
export function fillInputByPlaceholder(
  placeholder: string | RegExp,
  value: string | number
): void {
  const input = screen.getByPlaceholderText(placeholder)
  fillInput(input, value)
}

/**
 * Fill input by role - useful for comboboxes and other role-based inputs.
 *
 * @example
 * fillInputByRole('spinbutton', { name: /amount/i }, '100')
 */
export function fillInputByRole(
  role: string,
  options: { name: string | RegExp },
  value: string | number
): void {
  const input = screen.getByRole(role, options)
  fillInput(input, value)
}

/**
 * Clear and fill input in single operation.
 * Replaces slow user.clear() + user.type() pattern.
 *
 * @example
 * clearAndFill(amountInput, '5000')
 */
export function clearAndFill(element: HTMLElement, value: string | number): void {
  fillInput(element, value)
}

/**
 * Fill a date input (type="date").
 * Format should be YYYY-MM-DD.
 *
 * @example
 * fillDateInput(screen.getByLabelText(/start date/i), '2025-01-01')
 */
export function fillDateInput(element: HTMLElement, dateString: string): void {
  fireEvent.change(element, { target: { value: dateString } })
}

/**
 * Fill date input by label.
 *
 * @example
 * fillDateInputByLabel(/start date/i, '2025-01-01')
 */
export function fillDateInputByLabel(
  labelText: string | RegExp,
  dateString: string
): void {
  const input = screen.getByLabelText(labelText)
  fillDateInput(input, dateString)
}

/**
 * Select an option from a native HTML select element using fireEvent.
 * Faster than userEvent.selectOptions().
 *
 * @example
 * selectOption(screen.getByLabelText(/period/i), 'monthly')
 */
export function selectOption(element: HTMLElement, value: string): void {
  fireEvent.change(element, { target: { value } })
}

/**
 * Select from native select by label.
 *
 * @example
 * selectOptionByLabel(/period/i, 'monthly')
 */
export function selectOptionByLabel(
  labelText: string | RegExp,
  value: string
): void {
  const select = screen.getByLabelText(labelText)
  selectOption(select, value)
}

/**
 * Select from a SearchableSelect component (custom dropdown).
 * Opens the dropdown and clicks the option.
 *
 * @example
 * await selectSearchableOption(/target category/i, 'Food & Drink > Coffee')
 */
export async function selectSearchableOption(
  comboboxName: string | RegExp,
  optionText: string
): Promise<void> {
  const combobox = screen.getByRole('combobox', { name: comboboxName })
  fireEvent.click(combobox)

  // Wait for the option to appear and click it
  const option = await screen.findByText(optionText)
  fireEvent.click(option)
}

/**
 * waitFor with shorter default timeout for fast operations.
 * Use when you know the operation should complete quickly.
 *
 * @example
 * await waitForFast(() => expect(element).toBeVisible())
 */
export function waitForFast<T>(
  callback: () => T | Promise<T>,
  timeout = 500
): Promise<T> {
  return waitFor(callback, { timeout })
}

/**
 * Fill multiple form fields at once using a record of label patterns to values.
 *
 * @example
 * fillForm({
 *   'name': 'Test Budget',
 *   'amount': '2000',
 *   'description': 'My budget'
 * })
 */
export function fillForm(fields: Record<string, string | number>): void {
  Object.entries(fields).forEach(([label, value]) => {
    fillInputByLabel(new RegExp(label, 'i'), value)
  })
}
