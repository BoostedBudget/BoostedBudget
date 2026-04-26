/**
 * CreateBudgetDialog
 * Issue #111: Goals Dashboard & Progress Visualization
 * Issue #462: Migrated from HeadlessUI to shadcn/ui
 * Issue #496: Income Source Configuration UI for Envelope Budgeting
 * Issue #1048: Unified single-form design (removed tabs; Expected Income first-class field)
 *
 * Modal dialog for creating budgets with period selection and income sources.
 * Uses shadcn/ui Dialog built on Radix UI.
 */

import { useState, useEffect, useCallback } from 'react';
import { cn } from '@/lib/utils';
import { XIcon, ChevronDownIcon, ChevronRightIcon } from 'lucide-react';
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { Button } from '@/components/ui/button';
import { Alert } from '@/components/ui/alert';
import { useCreateBudget } from '@boostedbudget/api-client/endpoints/budgets/budgets';
import { useGetPlaidPrimaryCategories } from '@boostedbudget/api-client/endpoints/categories/categories'
import type { Primary } from '@boostedbudget/api-client/models/primary';
import { useGetUserTags } from '@boostedbudget/api-client/endpoints/tags/tags';
import type { UserTag } from '@/features/tags/types';
import type { Budget, BudgetPeriod, CreateBudgetRequest, IncomeSource } from '../types';
import { IncomeSourceConfig } from './income';

export interface CreateBudgetDialogProps {
  isOpen: boolean;
  onCancel: () => void;
  onBudgetCreated: (budget: Budget) => void;
}

interface FormData {
  name: string;
  description: string;
  period: BudgetPeriod;
  expectedIncome: string;
  start_date: string;
  end_date: string;
  income_sources: IncomeSource[];
}

interface FormErrors {
  name?: string;
  expectedIncome?: string;
  end_date?: string;
}

const PERIOD_OPTIONS: { value: BudgetPeriod; label: string }[] = [
  { value: 'weekly', label: 'Weekly' },
  { value: 'monthly', label: 'Monthly' },
  { value: 'quarterly', label: 'Quarterly' },
  { value: 'yearly', label: 'Yearly' },
  { value: 'custom', label: 'Custom' },
];

const PERIOD_LABEL: Record<BudgetPeriod, string> = {
  weekly: 'per week',
  monthly: 'per month',
  quarterly: 'per quarter',
  yearly: 'per year',
  custom: 'for period',
};

function getDefaultStartDate(): string {
  return new Date().toISOString().split('T')[0];
}

const initialFormData: FormData = {
  name: '',
  description: '',
  period: 'monthly',
  expectedIncome: '',
  start_date: getDefaultStartDate(),
  end_date: '',
  income_sources: [],
};

export function CreateBudgetDialog({
  isOpen,
  onCancel,
  onBudgetCreated,
}: CreateBudgetDialogProps) {
  const [formData, setFormData] = useState<FormData>(initialFormData);
  const [errors, setErrors] = useState<FormErrors>({});
  const [apiError, setApiError] = useState<string | null>(null);
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [trackingOpen, setTrackingOpen] = useState(false);

  const createBudgetMutation = useCreateBudget();
  const { data: plaidPrimariesData } = useGetPlaidPrimaryCategories({
    query: { enabled: isOpen },
  });
  const { data: userTagsData } = useGetUserTags({
    query: { enabled: isOpen },
  });

  const allPlaidPrimaries = (plaidPrimariesData as unknown as Primary[]) ?? [];
  // Filter to income-relevant categories only
  const plaidCategories = allPlaidPrimaries.filter(p => p.enum === 'INCOME' || p.enum === 'TRANSFER_IN');
  const userTags = (userTagsData as unknown as UserTag[]) ?? [];

  // Reset form when dialog closes
  useEffect(() => {
    if (!isOpen) {
      setFormData({ ...initialFormData, start_date: getDefaultStartDate() });
      setErrors({});
      setApiError(null);
      setTrackingOpen(false);
    }
  }, [isOpen]);

  const handleInputChange = useCallback(
    (e: React.ChangeEvent<HTMLInputElement | HTMLTextAreaElement | HTMLSelectElement>) => {
      const { name, value } = e.target;
      setFormData((prev) => ({ ...prev, [name]: value }));
      // Clear error for this field when user edits
      if (errors[name as keyof FormErrors]) {
        setErrors((prev) => ({ ...prev, [name]: undefined }));
      }
    },
    [errors]
  );

  const handleIncomeSourcesChange = useCallback((sources: IncomeSource[]) => {
    setFormData((prev) => ({ ...prev, income_sources: sources }));
  }, []);

  const validate = useCallback((): boolean => {
    const newErrors: FormErrors = {};

    if (!formData.name.trim()) {
      newErrors.name = 'Name is required';
    }

    const income = parseFloat(formData.expectedIncome);
    if (isNaN(income) || income <= 0) {
      newErrors.expectedIncome = 'Expected income must be greater than 0';
    }

    if (formData.period === 'custom' && !formData.end_date) {
      newErrors.end_date = 'End date is required for custom period';
    }

    setErrors(newErrors);
    return Object.keys(newErrors).length === 0;
  }, [formData]);

  const handleSubmit = useCallback(
    async (e: React.FormEvent) => {
      e.preventDefault();
      setApiError(null);

      if (!validate()) {
        return;
      }

      setIsSubmitting(true);

      try {
        const incomeValue = parseFloat(formData.expectedIncome);
        const request: CreateBudgetRequest = {
          name: formData.name.trim(),
          description: formData.description.trim() || undefined,
          period: formData.period,
          start_date: new Date(formData.start_date).toISOString(),
          end_date: formData.period === 'custom'
            ? new Date(formData.end_date).toISOString()
            : undefined,
          // Belt-and-suspenders: send both for back-compat
          expected_income: incomeValue,
          amount: incomeValue,
          income_sources: formData.income_sources.length > 0 ? formData.income_sources : undefined,
        };

        const newBudget = await createBudgetMutation.mutateAsync({
          data: request as never,
        });
        onBudgetCreated(newBudget as unknown as Budget);
      } catch (err) {
        setApiError(err instanceof Error ? err.message : 'Failed to create budget');
      } finally {
        setIsSubmitting(false);
      }
    },
    [formData, validate, onBudgetCreated, createBudgetMutation]
  );

  const handleOpenChange = useCallback(
    (open: boolean) => {
      if (!open) {
        onCancel();
      }
    },
    [onCancel]
  );

  const periodLabel = PERIOD_LABEL[formData.period] ?? 'per period';

  return (
    <Dialog open={isOpen} onOpenChange={handleOpenChange}>
      <DialogContent
        className="create-budget-dialog w-full max-w-lg p-6 bg-card rounded-2xl"
        // Issue #460: Prevent click-outside-to-close to avoid accidental data loss
        onInteractOutside={(e) => e.preventDefault()}
        onPointerDownOutside={(e) => e.preventDefault()}
        showCloseButton={false}
      >
        <DialogHeader className="flex flex-row items-center justify-between mb-6">
          <DialogTitle className="text-xl font-semibold text-foreground">
            Create Budget
          </DialogTitle>
          <button
            type="button"
            onClick={onCancel}
            className="text-muted-foreground hover:text-foreground"
          >
            <XIcon className="h-6 w-6" />
          </button>
        </DialogHeader>

        <form onSubmit={handleSubmit} className="space-y-4">
          {/* Section: Basics */}
          <div className="space-y-4">
            {/* Name */}
            <div>
              <label htmlFor="name" className="block text-sm font-medium text-foreground">
                Name
              </label>
              <input
                type="text"
                id="name"
                name="name"
                value={formData.name}
                onChange={handleInputChange}
                className={cn(
                  'w-full py-[0.625rem] px-3 mt-1 border border-input rounded-lg text-sm bg-card text-foreground transition-[border-color,box-shadow] focus:outline-none focus:border-primary focus:ring-2 focus:ring-primary/10 placeholder:text-muted-foreground',
                  errors.name && 'border-destructive focus:border-destructive focus:ring-destructive/10'
                )}
                placeholder="e.g., Monthly Budget"
              />
              {errors.name && (
                <p className="mt-1 text-sm text-destructive">{errors.name}</p>
              )}
            </div>

            {/* Description */}
            <div>
              <label htmlFor="description" className="block text-sm font-medium text-foreground">
                Description
              </label>
              <textarea
                id="description"
                name="description"
                value={formData.description}
                onChange={handleInputChange}
                rows={2}
                className="w-full py-[0.625rem] px-3 mt-1 border border-input rounded-lg text-sm bg-card text-foreground transition-[border-color,box-shadow] focus:outline-none focus:border-primary focus:ring-2 focus:ring-primary/10 placeholder:text-muted-foreground"
                placeholder="What is this budget for?"
              />
            </div>

            {/* Period */}
            <div>
              <label htmlFor="period" className="block text-sm font-medium text-foreground">
                Period
              </label>
              <select
                id="period"
                name="period"
                value={formData.period}
                onChange={handleInputChange}
                className="w-full py-[0.625rem] px-3 mt-1 border border-input rounded-lg text-sm bg-card text-foreground transition-[border-color,box-shadow] focus:outline-none focus:border-primary focus:ring-2 focus:ring-primary/10 placeholder:text-muted-foreground"
              >
                {PERIOD_OPTIONS.map((option) => (
                  <option key={option.value} value={option.value}>
                    {option.label}
                  </option>
                ))}
              </select>
            </div>

            {/* Start Date */}
            <div>
              <label htmlFor="start_date" className="block text-sm font-medium text-foreground">
                Start Date
              </label>
              <input
                type="date"
                id="start_date"
                name="start_date"
                value={formData.start_date}
                onChange={handleInputChange}
                className="w-full py-[0.625rem] px-3 mt-1 border border-input rounded-lg text-sm bg-card text-foreground transition-[border-color,box-shadow] focus:outline-none focus:border-primary focus:ring-2 focus:ring-primary/10 placeholder:text-muted-foreground"
              />
            </div>

            {/* End Date — only for custom period */}
            {formData.period === 'custom' && (
              <div>
                <label htmlFor="end_date" className="block text-sm font-medium text-foreground">
                  End Date
                </label>
                <input
                  type="date"
                  id="end_date"
                  name="end_date"
                  value={formData.end_date}
                  onChange={handleInputChange}
                  min={formData.start_date}
                  className={cn(
                    'w-full py-[0.625rem] px-3 mt-1 border border-input rounded-lg text-sm bg-card text-foreground transition-[border-color,box-shadow] focus:outline-none focus:border-primary focus:ring-2 focus:ring-primary/10 placeholder:text-muted-foreground',
                    errors.end_date && 'border-destructive focus:border-destructive focus:ring-destructive/10'
                  )}
                />
                {errors.end_date && (
                  <p className="mt-1 text-sm text-destructive">{errors.end_date}</p>
                )}
              </div>
            )}
          </div>

          {/* Section: Expected Income */}
          <div>
            <label htmlFor="expectedIncome" className="block text-sm font-medium text-foreground">
              Expected Income
              <span className="ml-1.5 text-xs font-normal text-muted-foreground">
                ({periodLabel})
              </span>
            </label>
            <div className="relative">
              <span className="absolute left-3 top-1/2 -translate-y-1/2 text-muted-foreground">
                $
              </span>
              <input
                type="number"
                id="expectedIncome"
                name="expectedIncome"
                value={formData.expectedIncome}
                onChange={handleInputChange}
                min="0"
                step="0.01"
                required
                className={cn(
                  'w-full py-[0.625rem] px-3 mt-1 pl-7 border border-input rounded-lg text-sm bg-card text-foreground transition-[border-color,box-shadow] focus:outline-none focus:border-primary focus:ring-2 focus:ring-primary/10 placeholder:text-muted-foreground',
                  errors.expectedIncome && 'border-destructive focus:border-destructive focus:ring-destructive/10'
                )}
                placeholder="0.00"
              />
            </div>
            {errors.expectedIncome && (
              <p className="mt-1 text-sm text-destructive">{errors.expectedIncome}</p>
            )}
          </div>

          {/* Section: Track actual income (optional, collapsed by default) */}
          <div className="border border-border rounded-lg overflow-hidden">
            <button
              type="button"
              onClick={() => setTrackingOpen((prev) => !prev)}
              className="flex items-center justify-between w-full px-4 py-3 text-sm font-medium text-foreground bg-muted hover:bg-muted/80 transition-colors"
              aria-expanded={trackingOpen}
            >
              <span>Track actual income from transactions</span>
              {trackingOpen ? (
                <ChevronDownIcon className="h-4 w-4 text-muted-foreground" />
              ) : (
                <ChevronRightIcon className="h-4 w-4 text-muted-foreground" />
              )}
            </button>

            {trackingOpen && (
              <div className="p-4 space-y-3">
                <p className="text-sm text-muted-foreground">
                  Link transaction categories or tags to automatically track income against your expected amount.
                </p>
                <IncomeSourceConfig
                  sources={formData.income_sources}
                  onChange={handleIncomeSourcesChange}
                  availableCategories={plaidCategories}
                  availableTags={userTags}
                  disabled={isSubmitting}
                  showTotal={false}
                />
              </div>
            )}
          </div>

          {/* API Error */}
          {apiError && (
            <Alert variant="destructive">{apiError}</Alert>
          )}

          {/* Actions */}
          <div className="flex justify-end gap-3 pt-4">
            <Button variant="outline" type="button" onClick={onCancel}>
              Cancel
            </Button>
            <Button type="submit" disabled={isSubmitting}>
              {isSubmitting ? 'Creating...' : 'Create Budget'}
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}
