/**
 * CustomCategoryListItem tests (Issue #1203).
 */
import { describe, it, expect, mock } from 'bun:test'
import { render, screen, fireEvent } from '@testing-library/react'
import { CustomCategoryListItem } from './CustomCategoryListItem'
import type { CustomCategory } from '../types'
import { scanForUnmaskedPII, type PIISentinel } from '@/test/pii-masking'

function cat(p: Partial<CustomCategory> & { id: string; name: string }): CustomCategory {
  return { user_id: 'u1', usage_count: 0, created_at: '', updated_at: '', ...p }
}

describe('CustomCategoryListItem', () => {
  it('renders the category name and edit/delete controls', () => {
    render(
      <CustomCategoryListItem category={cat({ id: 'c1', name: 'Coffee' })} onEdit={() => {}} onDelete={() => {}} />
    )
    expect(screen.getByText('Coffee')).toBeTruthy()
    expect(screen.getByRole('button', { name: /edit coffee/i })).toBeTruthy()
    expect(screen.getByRole('button', { name: /delete coffee/i })).toBeTruthy()
  })

  it('fires onEdit and onDelete when the action buttons are clicked', () => {
    const onEdit = mock(() => {})
    const onDelete = mock(() => {})
    const c = cat({ id: 'c1', name: 'Coffee' })
    render(<CustomCategoryListItem category={c} onEdit={onEdit} onDelete={onDelete} />)

    fireEvent.click(screen.getByRole('button', { name: /edit coffee/i }))
    fireEvent.click(screen.getByRole('button', { name: /delete coffee/i }))

    expect(onEdit).toHaveBeenCalledTimes(1)
    expect(onDelete).toHaveBeenCalledTimes(1)
  })

  it('invokes onKeyDown for keys on the row itself', () => {
    const onKeyDown = mock(() => {})
    render(
      <CustomCategoryListItem
        category={cat({ id: 'c1', name: 'Coffee' })}
        onEdit={() => {}}
        onDelete={() => {}}
        onKeyDown={onKeyDown}
      />
    )
    const row = screen.getByRole('listitem')
    fireEvent.keyDown(row, { key: 'ArrowDown' })
    expect(onKeyDown).toHaveBeenCalledTimes(1)
  })

  it('does NOT fire the row onKeyDown when keys bubble up from a nested action button', () => {
    // Regression: Enter/Delete on the edit/delete buttons must not also trigger row actions.
    const onKeyDown = mock(() => {})
    render(
      <CustomCategoryListItem
        category={cat({ id: 'c1', name: 'Coffee' })}
        onEdit={() => {}}
        onDelete={() => {}}
        onKeyDown={onKeyDown}
      />
    )
    fireEvent.keyDown(screen.getByRole('button', { name: /delete coffee/i }), { key: 'Delete' })
    expect(onKeyDown).not.toHaveBeenCalled()
  })

  describe('PII: category name never appears in an unmaskable DOM attribute (issue #1514, G5)', () => {
    // getByRole({ name: /edit coffee/i }) passes whether "Coffee" comes from a raw
    // aria-label attribute or from aria-labelledby pointing at masked text — it can't tell
    // the two apart. This asserts on the actual attribute values (what PostHog session
    // replay records verbatim), so it fails against the pre-fix
    // `aria-label={`Edit ${category.name}`}` implementation.
    it('does not leak the category name via any attribute on the Edit/Delete buttons', () => {
      const { container } = render(
        <CustomCategoryListItem category={cat({ id: 'c1', name: 'Coffee' })} onEdit={() => {}} onDelete={() => {}} />
      )

      const editButton = screen.getByRole('button', { name: /edit coffee/i })
      const deleteButton = screen.getByRole('button', { name: /delete coffee/i })
      for (const btn of [editButton, deleteButton]) {
        for (const attr of Array.from(btn.attributes)) {
          expect(attr.value).not.toContain('Coffee')
        }
      }

      const sentinels: PIISentinel[] = [{ value: 'Coffee', fieldDescription: 'category.name' }]
      expect(scanForUnmaskedPII(container, sentinels)).toEqual([])
    })
  })

  it('indents the child variant', () => {
    render(
      <CustomCategoryListItem category={cat({ id: 'c2', name: 'Latte' })} variant="child" onEdit={() => {}} onDelete={() => {}} />
    )
    expect(screen.getByRole('listitem').className).toContain('ml-6')
  })
})
