import { describe, it, expect } from 'vitest'
import { REGIONS, getRegionDisplay, getRegionInfo, isValidRegion, REGION_CODES, DEFAULT_REGION } from './regions'

describe('Region Configuration', () => {
  describe('REGIONS constant', () => {
    it('should have exactly 3 regions defined', () => {
      const regionKeys = Object.keys(REGIONS)
      expect(regionKeys).toHaveLength(3)
    })

    it('should include iad region with correct details', () => {
      expect(REGIONS.iad).toBeDefined()
      expect(REGIONS.iad.code).toBe('iad')
      expect(REGIONS.iad.displayName).toBe('USA (Virginia)')
      expect(REGIONS.iad.country).toBe('USA')
      expect(REGIONS.iad.city).toBe('Virginia')
    })

    it('should include lhr region with correct details', () => {
      expect(REGIONS.lhr).toBeDefined()
      expect(REGIONS.lhr.code).toBe('lhr')
      expect(REGIONS.lhr.displayName).toBe('UK (London)')
      expect(REGIONS.lhr.country).toBe('UK')
      expect(REGIONS.lhr.city).toBe('London')
    })

    it('should include syd region with correct details', () => {
      expect(REGIONS.syd).toBeDefined()
      expect(REGIONS.syd.code).toBe('syd')
      expect(REGIONS.syd.displayName).toBe('Australia (Sydney)')
      expect(REGIONS.syd.country).toBe('Australia')
      expect(REGIONS.syd.city).toBe('Sydney')
    })

    it('should not include old AWS-style region codes', () => {
      // @ts-expect-error - Testing that old codes don't exist
      expect(REGIONS['us-east-1']).toBeUndefined()
      // @ts-expect-error
      expect(REGIONS['eu-west-1']).toBeUndefined()
      // @ts-expect-error
      expect(REGIONS['ap-southeast-1']).toBeUndefined()
    })
  })

  describe('getRegionDisplay', () => {
    it('should return display name for valid iad region', () => {
      expect(getRegionDisplay('iad')).toBe('USA (Virginia)')
    })

    it('should return display name for valid lhr region', () => {
      expect(getRegionDisplay('lhr')).toBe('UK (London)')
    })

    it('should return display name for valid syd region', () => {
      expect(getRegionDisplay('syd')).toBe('Australia (Sydney)')
    })

    it('should return code itself for invalid region (graceful degradation)', () => {
      expect(getRegionDisplay('invalid')).toBe('invalid')
    })

    it('should return old AWS codes as-is (not found)', () => {
      expect(getRegionDisplay('us-east-1')).toBe('us-east-1')
      expect(getRegionDisplay('eu-west-1')).toBe('eu-west-1')
    })

    it('should return empty string for empty input', () => {
      expect(getRegionDisplay('')).toBe('')
    })
  })

  describe('getRegionInfo', () => {
    it('should return full region info for valid code', () => {
      const info = getRegionInfo('iad')
      expect(info).toBeDefined()
      expect(info?.code).toBe('iad')
      expect(info?.displayName).toBe('USA (Virginia)')
      expect(info?.country).toBe('USA')
      expect(info?.city).toBe('Virginia')
    })

    it('should return undefined for invalid code', () => {
      expect(getRegionInfo('invalid')).toBeUndefined()
    })

    it('should return undefined for old AWS codes', () => {
      expect(getRegionInfo('us-east-1')).toBeUndefined()
      expect(getRegionInfo('eu-west-1')).toBeUndefined()
    })
  })

  describe('isValidRegion', () => {
    it('should return true for valid iad code', () => {
      expect(isValidRegion('iad')).toBe(true)
    })

    it('should return true for valid lhr code', () => {
      expect(isValidRegion('lhr')).toBe(true)
    })

    it('should return true for valid syd code', () => {
      expect(isValidRegion('syd')).toBe(true)
    })

    it('should return false for invalid code', () => {
      expect(isValidRegion('invalid')).toBe(false)
    })

    it('should return false for old AWS codes', () => {
      expect(isValidRegion('us-east-1')).toBe(false)
      expect(isValidRegion('eu-west-1')).toBe(false)
      expect(isValidRegion('ap-southeast-1')).toBe(false)
    })

    it('should return false for empty string', () => {
      expect(isValidRegion('')).toBe(false)
    })
  })

  describe('REGION_CODES constant', () => {
    it('should be an array of exactly 3 region codes', () => {
      expect(REGION_CODES).toHaveLength(3)
    })

    it('should include iad, lhr, and syd', () => {
      expect(REGION_CODES).toContain('iad')
      expect(REGION_CODES).toContain('lhr')
      expect(REGION_CODES).toContain('syd')
    })

    it('should not include old AWS codes', () => {
      expect(REGION_CODES).not.toContain('us-east-1')
      expect(REGION_CODES).not.toContain('eu-west-1')
      expect(REGION_CODES).not.toContain('ap-southeast-1')
    })
  })

  describe('DEFAULT_REGION constant', () => {
    it('should be iad (US East Virginia)', () => {
      expect(DEFAULT_REGION).toBe('iad')
    })

    it('should be a valid region', () => {
      expect(isValidRegion(DEFAULT_REGION)).toBe(true)
    })
  })
})
