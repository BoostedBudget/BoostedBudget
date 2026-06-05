import { describe, it, expect, mock } from 'bun:test'
import { render, screen, fireEvent } from '@testing-library/react'
import { ReviewFilterPresets } from './ReviewFilterPresets'
import { presetToFilters } from './reviewFilterPresetsUtils'

describe('ReviewFilterPresets', () => {
  it('renders all four preset buttons', () => {
    render(<ReviewFilterPresets activePreset="unreviewed" onChange={mock()} />)
    expect(screen.getByText('Unreviewed')).toBeInTheDocument()
    expect(screen.getByText('Reviewed')).toBeInTheDocument()
    expect(screen.getByText('Auto-categorized')).toBeInTheDocument()
    expect(screen.getByText('Manually reviewed')).toBeInTheDocument()
  })

  it('calls onChange with correct filters when clicking a preset', () => {
    const onChange = mock()
    render(<ReviewFilterPresets activePreset="unreviewed" onChange={onChange} />)
    fireEvent.click(screen.getByText('Reviewed'))
    expect(onChange).toHaveBeenCalledWith('reviewed', { reviewed: true, category_sources: undefined })
  })

  it('calls onChange with category_sources for auto-categorized preset', () => {
    const onChange = mock()
    render(<ReviewFilterPresets activePreset="unreviewed" onChange={onChange} />)
    fireEvent.click(screen.getByText('Auto-categorized'))
    expect(onChange).toHaveBeenCalledWith(
      'auto-categorized',
      expect.objectContaining({ reviewed: false, category_sources: expect.arrayContaining(['plaid', 'rule']) }),
    )
  })
})

describe('presetToFilters', () => {
  it('returns reviewed:false for unreviewed', () => {
    expect(presetToFilters('unreviewed')).toEqual({ reviewed: false, category_sources: undefined })
  })

  it('returns reviewed:true for reviewed', () => {
    expect(presetToFilters('reviewed')).toEqual({ reviewed: true, category_sources: undefined })
  })

  it('returns plaid+rule sources for auto-categorized', () => {
    const f = presetToFilters('auto-categorized')
    expect(f.reviewed).toBe(false)
    expect(f.category_sources).toContain('plaid')
    expect(f.category_sources).toContain('rule')
  })

  it('returns manual source for manually-reviewed', () => {
    const f = presetToFilters('manually-reviewed')
    expect(f.reviewed).toBe(true)
    expect(f.category_sources).toContain('manual')
  })
})
