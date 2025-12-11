/**
 * goalTemplates Tests
 * Issue #111: Goals Dashboard & Progress Visualization
 *
 * TDD tests for goal template utilities.
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import {
  GOAL_TEMPLATES,
  getGoalTemplate,
  calculateDeadlineFromTemplate,
  type GoalTemplate,
} from './goalTemplates';

describe('goalTemplates', () => {
  // ==========================================================================
  // GOAL_TEMPLATES constant tests
  // ==========================================================================

  describe('GOAL_TEMPLATES', () => {
    it('exports 6 templates', () => {
      expect(GOAL_TEMPLATES).toHaveLength(6);
    });

    it('each template has required fields', () => {
      GOAL_TEMPLATES.forEach((template) => {
        expect(template).toHaveProperty('id');
        expect(template).toHaveProperty('name');
        expect(template).toHaveProperty('description');
        expect(template).toHaveProperty('icon');
        expect(typeof template.id).toBe('string');
        expect(typeof template.name).toBe('string');
        expect(typeof template.description).toBe('string');
        expect(typeof template.icon).toBe('string');
      });
    });

    it('includes emergency-fund template with 12 month deadline', () => {
      const emergencyFund = GOAL_TEMPLATES.find((t) => t.id === 'emergency-fund');
      expect(emergencyFund).toBeDefined();
      expect(emergencyFund?.suggestedDeadlineMonths).toBe(12);
    });

    it('includes vacation template with 6 month deadline', () => {
      const vacation = GOAL_TEMPLATES.find((t) => t.id === 'vacation');
      expect(vacation).toBeDefined();
      expect(vacation?.suggestedDeadlineMonths).toBe(6);
    });

    it('includes debt-payoff template without deadline', () => {
      const debtPayoff = GOAL_TEMPLATES.find((t) => t.id === 'debt-payoff');
      expect(debtPayoff).toBeDefined();
      expect(debtPayoff?.suggestedDeadlineMonths).toBeUndefined();
    });

    it('includes custom template without deadline', () => {
      const custom = GOAL_TEMPLATES.find((t) => t.id === 'custom');
      expect(custom).toBeDefined();
      expect(custom?.suggestedDeadlineMonths).toBeUndefined();
    });
  });

  // ==========================================================================
  // getGoalTemplate tests
  // ==========================================================================

  describe('getGoalTemplate', () => {
    it('returns template for valid ID', () => {
      const template = getGoalTemplate('emergency-fund');
      expect(template).toBeDefined();
      expect(template?.id).toBe('emergency-fund');
      expect(template?.name).toBe('Emergency Fund');
    });

    it('returns undefined for invalid ID', () => {
      const template = getGoalTemplate('non-existent-template');
      expect(template).toBeUndefined();
    });

    it('returns undefined for empty string ID', () => {
      const template = getGoalTemplate('');
      expect(template).toBeUndefined();
    });

    it('returns emergency-fund template correctly', () => {
      const template = getGoalTemplate('emergency-fund');
      expect(template).toEqual({
        id: 'emergency-fund',
        name: 'Emergency Fund',
        description: 'Build 3-6 months of expenses for unexpected situations',
        icon: 'shield-check',
        suggestedDeadlineMonths: 12,
      });
    });

    it('returns custom template correctly', () => {
      const template = getGoalTemplate('custom');
      expect(template).toEqual({
        id: 'custom',
        name: 'Custom Goal',
        description: 'Create your own savings goal',
        icon: 'plus',
      });
    });

    it('returns vacation template correctly', () => {
      const template = getGoalTemplate('vacation');
      expect(template?.id).toBe('vacation');
      expect(template?.suggestedDeadlineMonths).toBe(6);
    });

    it('returns home-down-payment template with 36 month deadline', () => {
      const template = getGoalTemplate('home-down-payment');
      expect(template?.id).toBe('home-down-payment');
      expect(template?.suggestedDeadlineMonths).toBe(36);
    });

    it('returns car-purchase template with 24 month deadline', () => {
      const template = getGoalTemplate('car-purchase');
      expect(template?.id).toBe('car-purchase');
      expect(template?.suggestedDeadlineMonths).toBe(24);
    });
  });

  // ==========================================================================
  // calculateDeadlineFromTemplate tests
  // ==========================================================================

  describe('calculateDeadlineFromTemplate', () => {
    // Mock Date for predictable testing
    const mockDate = new Date('2025-06-15T12:00:00Z');

    beforeEach(() => {
      vi.useFakeTimers();
      vi.setSystemTime(mockDate);
    });

    afterEach(() => {
      vi.useRealTimers();
    });

    it('returns date string for template with suggestedDeadlineMonths', () => {
      const template: GoalTemplate = {
        id: 'test',
        name: 'Test',
        description: 'Test template',
        icon: 'test',
        suggestedDeadlineMonths: 6,
      };

      const deadline = calculateDeadlineFromTemplate(template);
      expect(deadline).toBeDefined();
      expect(typeof deadline).toBe('string');
    });

    it('returns undefined for template without suggestedDeadlineMonths', () => {
      const template: GoalTemplate = {
        id: 'test',
        name: 'Test',
        description: 'Test template',
        icon: 'test',
      };

      const deadline = calculateDeadlineFromTemplate(template);
      expect(deadline).toBeUndefined();
    });

    it('calculates correct deadline for emergency-fund (12 months)', () => {
      const template = getGoalTemplate('emergency-fund')!;
      const deadline = calculateDeadlineFromTemplate(template);

      // June 2025 + 12 months = June 2026
      expect(deadline).toBe('2026-06-15');
    });

    it('calculates correct deadline for vacation (6 months)', () => {
      const template = getGoalTemplate('vacation')!;
      const deadline = calculateDeadlineFromTemplate(template);

      // June 2025 + 6 months = December 2025
      expect(deadline).toBe('2025-12-15');
    });

    it('returns undefined for debt-payoff template', () => {
      const template = getGoalTemplate('debt-payoff')!;
      const deadline = calculateDeadlineFromTemplate(template);
      expect(deadline).toBeUndefined();
    });

    it('returns undefined for custom template', () => {
      const template = getGoalTemplate('custom')!;
      const deadline = calculateDeadlineFromTemplate(template);
      expect(deadline).toBeUndefined();
    });

    it('returns date in YYYY-MM-DD format', () => {
      const template: GoalTemplate = {
        id: 'test',
        name: 'Test',
        description: 'Test template',
        icon: 'test',
        suggestedDeadlineMonths: 3,
      };

      const deadline = calculateDeadlineFromTemplate(template);
      expect(deadline).toMatch(/^\d{4}-\d{2}-\d{2}$/);
    });

    it('handles year boundary correctly (December + months)', () => {
      // Set date to December
      vi.setSystemTime(new Date('2025-12-15T12:00:00Z'));

      const template: GoalTemplate = {
        id: 'test',
        name: 'Test',
        description: 'Test template',
        icon: 'test',
        suggestedDeadlineMonths: 3,
      };

      const deadline = calculateDeadlineFromTemplate(template);
      // December 2025 + 3 months = March 2026
      expect(deadline).toBe('2026-03-15');
    });

    it('calculates home-down-payment deadline (36 months)', () => {
      const template = getGoalTemplate('home-down-payment')!;
      const deadline = calculateDeadlineFromTemplate(template);

      // June 2025 + 36 months = June 2028
      expect(deadline).toBe('2028-06-15');
    });

    it('calculates car-purchase deadline (24 months)', () => {
      const template = getGoalTemplate('car-purchase')!;
      const deadline = calculateDeadlineFromTemplate(template);

      // June 2025 + 24 months = June 2027
      expect(deadline).toBe('2027-06-15');
    });
  });
});
