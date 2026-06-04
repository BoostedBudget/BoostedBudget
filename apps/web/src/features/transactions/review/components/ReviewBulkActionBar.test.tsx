import { describe, it, expect, mock } from 'bun:test'
import { render, screen, fireEvent } from '@testing-library/react'
import { ReviewBulkActionBar } from './ReviewBulkActionBar'

describe('ReviewBulkActionBar', () => {
  it('renders nothing when selectedCount=0', () => {
    const { container } = render(
      <ReviewBulkActionBar
        selectedCount={0}
        onMarkReviewed={mock()}
        onMarkUnreviewed={mock()}
        onClear={mock()}
      />
    )
    expect(container.firstChild).toBeNull()
  })

  it('renders selection count', () => {
    render(
      <ReviewBulkActionBar
        selectedCount={3}
        onMarkReviewed={mock()}
        onMarkUnreviewed={mock()}
        onClear={mock()}
      />
    )
    expect(screen.getByText('3 selected')).toBeInTheDocument()
  })

  it('calls onMarkReviewed when "Mark reviewed" is clicked', () => {
    const onMarkReviewed = mock()
    render(
      <ReviewBulkActionBar
        selectedCount={2}
        onMarkReviewed={onMarkReviewed}
        onMarkUnreviewed={mock()}
        onClear={mock()}
      />
    )
    fireEvent.click(screen.getByLabelText('Mark selected as reviewed'))
    expect(onMarkReviewed).toHaveBeenCalledTimes(1)
  })

  it('calls onMarkUnreviewed when "Mark unreviewed" is clicked', () => {
    const onMarkUnreviewed = mock()
    render(
      <ReviewBulkActionBar
        selectedCount={1}
        onMarkReviewed={mock()}
        onMarkUnreviewed={onMarkUnreviewed}
        onClear={mock()}
      />
    )
    fireEvent.click(screen.getByLabelText('Mark selected as unreviewed'))
    expect(onMarkUnreviewed).toHaveBeenCalledTimes(1)
  })

  it('calls onClear when the clear button is clicked', () => {
    const onClear = mock()
    render(
      <ReviewBulkActionBar
        selectedCount={1}
        onMarkReviewed={mock()}
        onMarkUnreviewed={mock()}
        onClear={onClear}
      />
    )
    fireEvent.click(screen.getByLabelText('Clear selection'))
    expect(onClear).toHaveBeenCalledTimes(1)
  })
})
