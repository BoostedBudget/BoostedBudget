/**
 * Goal Templates
 * Issue #111: Goals Dashboard & Progress Visualization
 *
 * Pre-defined templates for common savings goals.
 */

export interface GoalTemplate {
  id: string;
  name: string;
  description: string;
  icon: string;
  suggestedDeadlineMonths?: number;
}

export const GOAL_TEMPLATES: GoalTemplate[] = [
  {
    id: 'emergency-fund',
    name: 'Emergency Fund',
    description: 'Build 3-6 months of expenses for unexpected situations',
    icon: 'shield-check',
    suggestedDeadlineMonths: 12,
  },
  {
    id: 'vacation',
    name: 'Vacation Fund',
    description: 'Save for your dream getaway',
    icon: 'sun',
    suggestedDeadlineMonths: 6,
  },
  {
    id: 'home-down-payment',
    name: 'Home Down Payment',
    description: 'Save for your future home',
    icon: 'home',
    suggestedDeadlineMonths: 36,
  },
  {
    id: 'car-purchase',
    name: 'Car Purchase',
    description: 'Save for a new vehicle',
    icon: 'truck',
    suggestedDeadlineMonths: 24,
  },
  {
    id: 'debt-payoff',
    name: 'Debt Payoff',
    description: 'Accelerate paying off debt',
    icon: 'credit-card',
  },
  {
    id: 'custom',
    name: 'Custom Goal',
    description: 'Create your own savings goal',
    icon: 'plus',
  },
];

/**
 * Get a template by its ID
 */
export function getGoalTemplate(id: string): GoalTemplate | undefined {
  return GOAL_TEMPLATES.find((template) => template.id === id);
}

/**
 * Calculate deadline date from suggested months
 */
export function calculateDeadlineFromTemplate(template: GoalTemplate): string | undefined {
  if (!template.suggestedDeadlineMonths) {
    return undefined;
  }
  const deadline = new Date();
  deadline.setMonth(deadline.getMonth() + template.suggestedDeadlineMonths);
  return deadline.toISOString().split('T')[0]; // YYYY-MM-DD format
}
