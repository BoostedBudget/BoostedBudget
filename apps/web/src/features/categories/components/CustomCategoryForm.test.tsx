/**
 * CustomCategoryForm tests (Issue #1203).
 */
import { describe, it, expect, mock } from 'bun:test'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'

// pfcOptions reads static data — no mock needed (mocking it pollutes the global suite).
import { CustomCategoryForm } from './CustomCategoryForm'
import type { CustomCategory } from '../types'

function cat(p: Partial<CustomCategory> & { id: string; name: string }): CustomCategory {
  return { user_id: 'u1', usage_count: 0, created_at: '', updated_at: '', ...p }
}

describe('CustomCategoryForm', () => {
  it('submits a top-level create with name', async () => {
    const onSubmit = mock(() => Promise.resolve())
    render(<CustomCategoryForm allCategories={[]} onSubmit={onSubmit} onCancel={() => {}} />)

    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'Coffee' } })
    fireEvent.click(screen.getByRole('button', { name: /create category/i }))

    await waitFor(() => expect(onSubmit).toHaveBeenCalledTimes(1))
    const req = (onSubmit.mock.calls[0] as unknown[])[0] as Record<string, unknown>
    expect(req.name).toBe('Coffee')
    expect(req.parent_pfc_primary).toBe('')
    expect(req.parent_id).toBe('')
  })

  it('submits create nested under a PFC primary', async () => {
    const onSubmit = mock(() => Promise.resolve())
    render(<CustomCategoryForm allCategories={[]} onSubmit={onSubmit} onCancel={() => {}} />)

    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'Bars' } })
    fireEvent.change(screen.getByLabelText('Nest under'), { target: { value: 'pfc:FOOD_AND_DRINK' } })
    fireEvent.click(screen.getByRole('button', { name: /create category/i }))

    await waitFor(() => expect(onSubmit).toHaveBeenCalledTimes(1))
    const req = (onSubmit.mock.calls[0] as unknown[])[0] as Record<string, unknown>
    expect(req.parent_pfc_primary).toBe('FOOD_AND_DRINK')
    expect(req.parent_id).toBe('')
  })

  it('submits create nested under a custom primary', async () => {
    const onSubmit = mock(() => Promise.resolve())
    const parents = [cat({ id: 'top1', name: 'Food' })]
    render(<CustomCategoryForm allCategories={parents} onSubmit={onSubmit} onCancel={() => {}} />)

    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'Fast Food' } })
    fireEvent.change(screen.getByLabelText('Nest under'), { target: { value: 'custom:top1' } })
    fireEvent.click(screen.getByRole('button', { name: /create category/i }))

    await waitFor(() => expect(onSubmit).toHaveBeenCalledTimes(1))
    const req = (onSubmit.mock.calls[0] as unknown[])[0] as Record<string, unknown>
    expect(req.parent_id).toBe('top1')
    expect(req.parent_pfc_primary).toBe('')
  })

  it('requires a name', async () => {
    const onSubmit = mock(() => Promise.resolve())
    render(<CustomCategoryForm allCategories={[]} onSubmit={onSubmit} onCancel={() => {}} />)
    fireEvent.click(screen.getByRole('button', { name: /create category/i }))
    expect(screen.getByRole('alert')).toBeTruthy()
    expect(onSubmit).not.toHaveBeenCalled()
  })

  it('excludes already-nested customs from the custom-parent options', () => {
    const cats = [
      cat({ id: 'top', name: 'Top' }),
      cat({ id: 'nested', name: 'Nested', parent_id: 'top' }),
    ]
    render(<CustomCategoryForm allCategories={cats} onSubmit={() => Promise.resolve()} onCancel={() => {}} />)
    const select = screen.getByLabelText('Nest under') as HTMLSelectElement
    const values = Array.from(select.querySelectorAll('option')).map((o) => o.value)
    expect(values).toContain('custom:top')
    expect(values).not.toContain('custom:nested')
  })
})
