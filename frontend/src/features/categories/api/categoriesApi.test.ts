import { describe, it, expect, beforeEach, afterEach } from 'vitest'
import MockAdapter from 'axios-mock-adapter'
import { apiClient } from '../../../api/axiosConfig'
import {
  fetchCategories,
  fetchCategoryById,
  createCategory,
  updateCategory,
  deleteCategory,
} from './categoriesApi'
import type { CustomCategory } from '../types'

describe('categoriesApi', () => {
  let mock: MockAdapter

  beforeEach(() => {
    mock = new MockAdapter(apiClient)
  })

  afterEach(() => {
    mock.reset()
  })

  const mockCategory: CustomCategory = {
    id: 'cat-1',
    user_id: 'user-1',
    name: 'Food & Dining',
    color: '#FF5733',
    usage_count: 10,
    created_at: '2025-01-01T00:00:00Z',
    updated_at: '2025-01-01T00:00:00Z',
  }

  describe('fetchCategories', () => {
    it('should fetch all categories', async () => {
      const mockCategories = [mockCategory]
      mock.onGet('/v1/categories').reply(200, mockCategories)

      const result = await fetchCategories()

      expect(result).toEqual(mockCategories)
      expect(mock.history.get[0].url).toBe('/v1/categories')
    })

    it('should handle empty categories array', async () => {
      mock.onGet('/v1/categories').reply(200, [])

      const result = await fetchCategories()

      expect(result).toEqual([])
    })

    it('should handle network errors', async () => {
      mock.onGet('/v1/categories').networkError()

      await expect(fetchCategories()).rejects.toThrow()
    })
  })

  describe('fetchCategoryById', () => {
    it('should fetch a category by ID', async () => {
      mock.onGet('/v1/categories/cat-1').reply(200, mockCategory)

      const result = await fetchCategoryById('cat-1')

      expect(result).toEqual(mockCategory)
      expect(mock.history.get[0].url).toBe('/v1/categories/cat-1')
    })

    it('should handle 404 not found', async () => {
      mock.onGet('/v1/categories/invalid').reply(404)

      await expect(fetchCategoryById('invalid')).rejects.toThrow()
    })
  })

  describe('createCategory', () => {
    it('should create a new category', async () => {
      mock.onPost('/v1/categories').reply(201, mockCategory)

      const result = await createCategory({ name: 'Food & Dining', color: '#FF5733' })

      expect(result).toEqual(mockCategory)
      expect(mock.history.post[0].url).toBe('/v1/categories')
      expect(JSON.parse(mock.history.post[0].data)).toEqual({
        name: 'Food & Dining',
        color: '#FF5733',
      })
    })

    it('should create category without color', async () => {
      const catNoColor = { ...mockCategory, color: null }
      mock.onPost('/v1/categories').reply(201, catNoColor)

      const result = await createCategory({ name: 'Food & Dining' })

      expect(result.color).toBeNull()
    })

    it('should handle validation errors', async () => {
      mock.onPost('/v1/categories').reply(400, { error: 'Name is required' })

      await expect(createCategory({ name: '' })).rejects.toThrow()
    })
  })

  describe('updateCategory', () => {
    it('should update a category', async () => {
      const updatedCategory = { ...mockCategory, name: 'Groceries' }
      mock.onPut('/v1/categories/cat-1').reply(200, updatedCategory)

      const result = await updateCategory('cat-1', { name: 'Groceries' })

      expect(result.name).toBe('Groceries')
      expect(mock.history.put[0].url).toBe('/v1/categories/cat-1')
    })

    it('should handle 404 not found', async () => {
      mock.onPut('/v1/categories/invalid').reply(404)

      await expect(updateCategory('invalid', { name: 'Test' })).rejects.toThrow()
    })
  })

  describe('deleteCategory', () => {
    it('should delete a category', async () => {
      mock.onDelete('/v1/categories/cat-1').reply(204)

      await expect(deleteCategory('cat-1')).resolves.toBeUndefined()
      expect(mock.history.delete[0].url).toBe('/v1/categories/cat-1')
    })

    it('should handle 404 not found', async () => {
      mock.onDelete('/v1/categories/invalid').reply(404)

      await expect(deleteCategory('invalid')).rejects.toThrow()
    })
  })
})
