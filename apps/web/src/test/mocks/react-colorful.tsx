/**
 * Centralized mock for react-colorful
 *
 * Issue #575: Created to prevent mock pollution between test files
 *
 * react-colorful uses canvas which isn't available in happy-dom.
 * This mock provides a simple implementation that works in tests.
 */

import { mock } from 'bun:test'

mock.module('react-colorful', () => ({
  HexColorPicker: ({
    color,
    onChange,
    style,
  }: {
    color: string
    onChange: (color: string) => void
    style?: React.CSSProperties
  }) => (
    <div
      data-testid="hex-color-picker"
      data-color={color}
      style={style}
      onClick={() => onChange('#FF0000')}
    />
  ),
}))
