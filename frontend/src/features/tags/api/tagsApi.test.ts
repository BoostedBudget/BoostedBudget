import { describe, it, expect, beforeEach, afterEach } from 'vitest'
import MockAdapter from 'axios-mock-adapter'
import { apiClient } from '../../../api/axiosConfig'
import { fetchTags, fetchTagById, createTag, updateTag, deleteTag } from './tagsApi'
import type { UserTag } from '../types'

describe('tagsApi', () => {
  let mock: MockAdapter

  beforeEach(() => {
    mock = new MockAdapter(apiClient)
  })

  afterEach(() => {
    mock.reset()
  })

  const mockTag: UserTag = {
    id: 'tag-1',
    user_id: 'user-1',
    name: 'Food',
    color: '#FF5733',
    usage_count: 5,
    created_at: '2025-01-01T00:00:00Z',
    updated_at: '2025-01-01T00:00:00Z',
  }

  describe('fetchTags', () => {
    it('should fetch all tags', async () => {
      const mockTags = [mockTag]
      mock.onGet('/v1/tags').reply(200, mockTags)

      const result = await fetchTags()

      expect(result).toEqual(mockTags)
      expect(mock.history.get[0].url).toBe('/v1/tags')
    })

    it('should handle empty tags array', async () => {
      mock.onGet('/v1/tags').reply(200, [])

      const result = await fetchTags()

      expect(result).toEqual([])
    })

    it('should handle network errors', async () => {
      mock.onGet('/v1/tags').networkError()

      await expect(fetchTags()).rejects.toThrow()
    })
  })

  describe('fetchTagById', () => {
    it('should fetch a tag by ID', async () => {
      mock.onGet('/v1/tags/tag-1').reply(200, mockTag)

      const result = await fetchTagById('tag-1')

      expect(result).toEqual(mockTag)
      expect(mock.history.get[0].url).toBe('/v1/tags/tag-1')
    })

    it('should handle 404 not found', async () => {
      mock.onGet('/v1/tags/invalid').reply(404)

      await expect(fetchTagById('invalid')).rejects.toThrow()
    })
  })

  describe('createTag', () => {
    it('should create a new tag', async () => {
      mock.onPost('/v1/tags').reply(201, mockTag)

      const result = await createTag({ name: 'Food', color: '#FF5733' })

      expect(result).toEqual(mockTag)
      expect(mock.history.post[0].url).toBe('/v1/tags')
      expect(JSON.parse(mock.history.post[0].data)).toEqual({ name: 'Food', color: '#FF5733' })
    })

    it('should create tag without color', async () => {
      const tagNoColor = { ...mockTag, color: null }
      mock.onPost('/v1/tags').reply(201, tagNoColor)

      const result = await createTag({ name: 'Food' })

      expect(result.color).toBeNull()
    })

    it('should handle validation errors', async () => {
      mock.onPost('/v1/tags').reply(400, { error: 'Name is required' })

      await expect(createTag({ name: '' })).rejects.toThrow()
    })
  })

  describe('updateTag', () => {
    it('should update a tag', async () => {
      const updatedTag = { ...mockTag, name: 'Groceries' }
      mock.onPut('/v1/tags/tag-1').reply(200, updatedTag)

      const result = await updateTag('tag-1', { name: 'Groceries' })

      expect(result.name).toBe('Groceries')
      expect(mock.history.put[0].url).toBe('/v1/tags/tag-1')
    })

    it('should handle 404 not found', async () => {
      mock.onPut('/v1/tags/invalid').reply(404)

      await expect(updateTag('invalid', { name: 'Test' })).rejects.toThrow()
    })
  })

  describe('deleteTag', () => {
    it('should delete a tag', async () => {
      mock.onDelete('/v1/tags/tag-1').reply(204)

      await expect(deleteTag('tag-1')).resolves.toBeUndefined()
      expect(mock.history.delete[0].url).toBe('/v1/tags/tag-1')
    })

    it('should handle 404 not found', async () => {
      mock.onDelete('/v1/tags/invalid').reply(404)

      await expect(deleteTag('invalid')).rejects.toThrow()
    })
  })
})
