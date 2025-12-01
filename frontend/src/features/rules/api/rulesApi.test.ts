import { describe, it, expect, beforeEach, afterEach } from 'vitest'
import MockAdapter from 'axios-mock-adapter'
import { apiClient } from '../../../api/axiosConfig'
import {
  fetchRules,
  fetchRuleById,
  createRule,
  updateRule,
  deleteRule,
  toggleRule,
} from './rulesApi'
import type { CategorizationRule, CreateRuleRequest } from '../types'

describe('rulesApi', () => {
  let mock: MockAdapter

  beforeEach(() => {
    mock = new MockAdapter(apiClient)
  })

  afterEach(() => {
    mock.reset()
  })

  const mockRule: CategorizationRule = {
    id: 'rule-1',
    user_id: 'user-1',
    name: 'Coffee Shops',
    description: 'Categorize coffee purchases',
    enabled: true,
    priority: 10,
    conditions: [
      { type: 'merchant_contains', value: 'starbucks' },
    ],
    condition_logic: 'AND',
    target_category: ['Food & Drink', 'Coffee'],
    target_tags: ['morning'],
    tag_behavior: 'add',
    source: 'manual',
    match_count: 5,
    override_count: 1,
    last_matched_at: '2025-01-15T10:00:00Z',
    created_at: '2025-01-01T00:00:00Z',
    updated_at: '2025-01-15T10:00:00Z',
  }

  describe('fetchRules', () => {
    it('should fetch all rules', async () => {
      const mockRules = [mockRule]
      mock.onGet('/v1/rules').reply(200, mockRules)

      const result = await fetchRules()

      expect(result).toEqual(mockRules)
      expect(mock.history.get[0].url).toBe('/v1/rules')
    })

    it('should handle empty rules array', async () => {
      mock.onGet('/v1/rules').reply(200, [])

      const result = await fetchRules()

      expect(result).toEqual([])
    })

    it('should handle network errors', async () => {
      mock.onGet('/v1/rules').networkError()

      await expect(fetchRules()).rejects.toThrow()
    })
  })

  describe('fetchRuleById', () => {
    it('should fetch a rule by ID', async () => {
      mock.onGet('/v1/rules/rule-1').reply(200, mockRule)

      const result = await fetchRuleById('rule-1')

      expect(result).toEqual(mockRule)
      expect(mock.history.get[0].url).toBe('/v1/rules/rule-1')
    })

    it('should handle 404 not found', async () => {
      mock.onGet('/v1/rules/invalid').reply(404)

      await expect(fetchRuleById('invalid')).rejects.toThrow()
    })
  })

  describe('createRule', () => {
    it('should create a new rule', async () => {
      mock.onPost('/v1/rules').reply(201, mockRule)

      const request: CreateRuleRequest = {
        name: 'Coffee Shops',
        conditions: [{ type: 'merchant_contains', value: 'starbucks' }],
        target_category: ['Food & Drink', 'Coffee'],
      }
      const result = await createRule(request)

      expect(result).toEqual(mockRule)
      expect(mock.history.post[0].url).toBe('/v1/rules')
      expect(JSON.parse(mock.history.post[0].data)).toEqual(request)
    })

    it('should create rule with tags only', async () => {
      const ruleWithTags = { ...mockRule, target_category: null }
      mock.onPost('/v1/rules').reply(201, ruleWithTags)

      const request: CreateRuleRequest = {
        name: 'Tag Rule',
        conditions: [{ type: 'merchant_contains', value: 'amazon' }],
        target_tags: ['shopping'],
      }
      const result = await createRule(request)

      expect(result.target_category).toBeNull()
      expect(result.target_tags).toEqual(['morning'])
    })

    it('should handle validation errors', async () => {
      mock.onPost('/v1/rules').reply(400, { error: 'Name is required' })

      await expect(
        createRule({ name: '', conditions: [] })
      ).rejects.toThrow()
    })

    it('should handle rule limit exceeded', async () => {
      mock.onPost('/v1/rules').reply(403, { error: 'Rule limit exceeded' })

      await expect(
        createRule({
          name: 'New Rule',
          conditions: [{ type: 'merchant_contains', value: 'test' }],
        })
      ).rejects.toThrow()
    })
  })

  describe('updateRule', () => {
    it('should update a rule', async () => {
      const updatedRule = { ...mockRule, name: 'Updated Name' }
      mock.onPut('/v1/rules/rule-1').reply(200, updatedRule)

      const result = await updateRule('rule-1', { name: 'Updated Name' })

      expect(result.name).toBe('Updated Name')
      expect(mock.history.put[0].url).toBe('/v1/rules/rule-1')
    })

    it('should update priority', async () => {
      const updatedRule = { ...mockRule, priority: 20 }
      mock.onPut('/v1/rules/rule-1').reply(200, updatedRule)

      const result = await updateRule('rule-1', { priority: 20 })

      expect(result.priority).toBe(20)
    })

    it('should handle 404 not found', async () => {
      mock.onPut('/v1/rules/invalid').reply(404)

      await expect(updateRule('invalid', { name: 'Test' })).rejects.toThrow()
    })
  })

  describe('deleteRule', () => {
    it('should delete a rule', async () => {
      mock.onDelete('/v1/rules/rule-1').reply(204)

      await expect(deleteRule('rule-1')).resolves.toBeUndefined()
      expect(mock.history.delete[0].url).toBe('/v1/rules/rule-1')
    })

    it('should handle 404 not found', async () => {
      mock.onDelete('/v1/rules/invalid').reply(404)

      await expect(deleteRule('invalid')).rejects.toThrow()
    })
  })

  describe('toggleRule', () => {
    it('should enable a rule', async () => {
      const enabledRule = { ...mockRule, enabled: true }
      mock.onPost('/v1/rules/rule-1/toggle').reply(200, enabledRule)

      const result = await toggleRule('rule-1', true)

      expect(result.enabled).toBe(true)
      expect(mock.history.post[0].url).toBe('/v1/rules/rule-1/toggle')
      expect(JSON.parse(mock.history.post[0].data)).toEqual({ enabled: true })
    })

    it('should disable a rule', async () => {
      const disabledRule = { ...mockRule, enabled: false }
      mock.onPost('/v1/rules/rule-1/toggle').reply(200, disabledRule)

      const result = await toggleRule('rule-1', false)

      expect(result.enabled).toBe(false)
    })

    it('should handle 404 not found', async () => {
      mock.onPost('/v1/rules/invalid/toggle').reply(404)

      await expect(toggleRule('invalid', true)).rejects.toThrow()
    })
  })
})
