import { describe, expect, it } from 'bun:test'
import { SERVER_INSTRUCTIONS } from './server-instructions'

describe('SERVER_INSTRUCTIONS', () => {
  it('names every recipe operation', () => {
    for (const operationId of [
      'listMCPRecipes',
      'getMCPRecipe',
      'createMCPRecipe',
      'updateMCPRecipe',
      'deleteMCPRecipe',
      'runMCPRecipe',
    ]) {
      expect(SERVER_INSTRUCTIONS).toContain(operationId)
    }
  })

  it('states the runMCPRecipe → submit-to-execute handoff', () => {
    expect(SERVER_INSTRUCTIONS).toContain('runMCPRecipe')
    expect(SERVER_INSTRUCTIONS).toContain('submit the returned calls')
    expect(SERVER_INSTRUCTIONS).toContain('readExecute')
    expect(SERVER_INSTRUCTIONS).toContain('writeExecute')
  })

  it('does not claim recipe scheduling exists', () => {
    expect(SERVER_INSTRUCTIONS).not.toMatch(/schedul|cron|automatically/i)
  })

  it('stays within the pinned 1024-byte cap', () => {
    expect(new TextEncoder().encode(SERVER_INSTRUCTIONS).byteLength).toBeLessThanOrEqual(1024)
  })
})
