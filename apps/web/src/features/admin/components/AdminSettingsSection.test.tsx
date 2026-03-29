/**
 * AdminSettingsSection Component Tests
 * Issue #642: Admin feature foundation
 */

import { describe, it, expect, mock } from 'bun:test'
import { render, screen, fireEvent } from '@testing-library/react'
import { AdminSettingsSection } from './AdminSettingsSection'

describe('AdminSettingsSection', () => {
  it('should render the admin settings section', () => {
    render(<AdminSettingsSection adminModeEnabled={false} onToggle={() => {}} />)

    expect(screen.getByText('Admin Settings')).toBeInTheDocument()
    expect(
      screen.getByText(/You have administrator privileges/i)
    ).toBeInTheDocument()
    expect(screen.getByText('Enable Admin Mode')).toBeInTheDocument()
  })

  it('should show checkbox unchecked when adminModeEnabled is false', () => {
    render(<AdminSettingsSection adminModeEnabled={false} onToggle={() => {}} />)

    const checkbox = screen.getByRole('checkbox')
    expect(checkbox).not.toBeChecked()
  })

  it('should show checkbox checked when adminModeEnabled is true', () => {
    render(<AdminSettingsSection adminModeEnabled={true} onToggle={() => {}} />)

    const checkbox = screen.getByRole('checkbox')
    expect(checkbox).toBeChecked()
  })

  it('should call onToggle when checkbox is clicked', () => {
    const onToggle = mock(() => {})
    render(<AdminSettingsSection adminModeEnabled={false} onToggle={onToggle} />)

    const checkbox = screen.getByRole('checkbox')
    fireEvent.click(checkbox)

    expect(onToggle).toHaveBeenCalledTimes(1)
  })

  it('should display help text about admin navigation', () => {
    render(<AdminSettingsSection adminModeEnabled={false} onToggle={() => {}} />)

    expect(
      screen.getByText(/admin navigation will appear in the header/i)
    ).toBeInTheDocument()
  })

  it('should have accessible label for checkbox', () => {
    render(<AdminSettingsSection adminModeEnabled={false} onToggle={() => {}} />)

    const checkbox = screen.getByRole('checkbox')
    expect(checkbox).toHaveAccessibleName('Enable Admin Mode')
  })
})
