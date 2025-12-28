/**
 * CreateBudgetDialog
 * Issue #111: Goals Dashboard & Progress Visualization
 * Issue #462: Migrated from HeadlessUI to shadcn/ui
 * Issue #496: Income Source Configuration UI for Envelope Budgeting
 *
 * Modal dialog for creating budgets with period selection and income sources.
 * Uses shadcn/ui Dialog and Tabs built on Radix UI.
 */

import { useState, useEffect, useCallback } from 'react';
import { XIcon } from 'lucide-react';
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui/tabs';
import { useCreateBudget } from '@/api/generated/endpoints/budgets/budgets';
import { useGetPlaidCategories } from '@/api/generated/endpoints/categories/categories';
import { useGetUserTags } from '@/api/generated/endpoints/tags/tags';
import type { UserTag } from '@/features/tags/types';
import type { Budget, BudgetPeriod, CreateBudgetRequest, IncomeSource } from '../types';
import { IncomeSourceConfig } from './income';
import './CreateBudgetDialog.css';

export interface CreateBudgetDialogProps {
  isOpen: boolean;
  onCancel: () => void;
  onBudgetCreated: (budget: Budget) => void;
}

interface FormData {
  name: string;
  description: string;
  period: BudgetPeriod;
  amount: string;
  start_date: string;
  end_date: string;
  // Issue #496: Income source fields
  income_sources: IncomeSource[];
  track_actual_income: boolean;
}

interface FormErrors {
  name?: string;
  amount?: string;
  end_date?: string;
}

const PERIOD_OPTIONS: { value: BudgetPeriod; label: string }[] = [
  { value: 'weekly', label: 'Weekly' },
  { value: 'monthly', label: 'Monthly' },
  { value: 'quarterly', label: 'Quarterly' },
  { value: 'yearly', label: 'Yearly' },
  { value: 'custom', label: 'Custom' },
];

function getDefaultStartDate(): string {
  return new Date().toISOString().split('T')[0];
}

const initialFormData: FormData = {
  name: '',
  description: '',
  period: 'monthly',
  amount: '',
  start_date: getDefaultStartDate(),
  end_date: '',
  // Issue #496: Income source defaults
  income_sources: [],
  track_actual_income: false,
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
  const [activeTab, setActiveTab] = useState<string>('budget');

  // Use generated hooks for data fetching
  const createBudgetMutation = useCreateBudget();
  const { data: plaidCategoriesData } = useGetPlaidCategories({
    query: { enabled: isOpen },
  });
  const { data: userTagsData } = useGetUserTags({
    query: { enabled: isOpen },
  });

  // Issue #496: Derived state for income source options
  const plaidCategories = (plaidCategoriesData as unknown as string[]) ?? [];
  const userTags = (userTagsData as unknown as UserTag[]) ?? [];

  // Reset form when dialog closes
  useEffect(() => {
    if (!isOpen) {
      setFormData({ ...initialFormData, start_date: getDefaultStartDate() });
      setErrors({});
      setApiError(null);
      setActiveTab('budget'); // Issue #496: Reset to budget tab
    }
  }, [isOpen]);

  const handleInputChange = useCallback(
    (e: React.ChangeEvent<HTMLInputElement | HTMLTextAreaElement | HTMLSelectElement>) => {
      const { name, value } = e.target;
      setFormData((prev) => ({ ...prev, [name]: value }));
      // Clear error for this field
      if (errors[name as keyof FormErrors]) {
        setErrors((prev) => ({ ...prev, [name]: undefined }));
      }
    },
    [errors]
  );

  // Issue #496: Handler for income sources
  const handleIncomeSourcesChange = useCallback((sources: IncomeSource[]) => {
    setFormData((prev) => ({ ...prev, income_sources: sources }));
  }, []);

  const validate = useCallback((): boolean => {
    const newErrors: FormErrors = {};

    if (!formData.name.trim()) {
      newErrors.name = 'Name is required';
    }

    const amount = parseFloat(formData.amount);
    if (isNaN(amount) || amount <= 0) {
      newErrors.amount = 'Amount must be greater than 0';
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
        const request: CreateBudgetRequest = {
          name: formData.name.trim(),
          description: formData.description.trim() || undefined,
          period: formData.period,
          // Convert date string (YYYY-MM-DD) to ISO 8601 datetime for Go time.Time parsing
          start_date: new Date(formData.start_date).toISOString(),
          end_date: formData.period === 'custom'
            ? new Date(formData.end_date).toISOString()
            : undefined,
          amount: parseFloat(formData.amount),
          // Issue #496: Include income sources
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

  // Handle dialog open state changes
  const handleOpenChange = useCallback(
    (open: boolean) => {
      if (!open) {
        onCancel();
      }
    },
    [onCancel]
  );

  return (
    <Dialog open={isOpen} onOpenChange={handleOpenChange}>
      <DialogContent
        className="create-budget-dialog w-full max-w-lg p-6 bg-white dark:bg-gray-900 rounded-2xl"
        // Issue #460: Prevent click-outside-to-close to avoid accidental data loss
        onInteractOutside={(e) => e.preventDefault()}
        onPointerDownOutside={(e) => e.preventDefault()}
        showCloseButton={false}
      >
        <DialogHeader className="flex flex-row items-center justify-between mb-6">
          <DialogTitle className="text-xl font-semibold text-gray-900 dark:text-white">
            Create Budget
          </DialogTitle>
          <button
            type="button"
            onClick={onCancel}
            className="text-gray-400 hover:text-gray-600 dark:hover:text-gray-300"
          >
            <XIcon className="h-6 w-6" />
          </button>
        </DialogHeader>

        <form onSubmit={handleSubmit} className="space-y-4">
          {/* Issue #496: Tabbed interface for Budget and Income */}
          <Tabs value={activeTab} onValueChange={setActiveTab}>
            <TabsList className="w-full">
              <TabsTrigger value="budget" className="flex-1">Budget</TabsTrigger>
              <TabsTrigger value="income" className="flex-1">Income</TabsTrigger>
            </TabsList>

            {/* Budget Tab */}
            <TabsContent value="budget" className="space-y-4">
              {/* Name Field */}
              <div>
                <label
                  htmlFor="name"
                  className="block text-sm font-medium text-gray-700 dark:text-gray-300"
                >
                  Name
                </label>
                <input
                  type="text"
                  id="name"
                  name="name"
                  value={formData.name}
                  onChange={handleInputChange}
                  className={`create-budget-dialog__input ${
                    errors.name ? 'create-budget-dialog__input--error' : ''
                  }`}
                  placeholder="e.g., Monthly Budget"
                />
                {errors.name && (
                  <p className="mt-1 text-sm text-red-600 dark:text-red-400">
                    {errors.name}
                  </p>
                )}
              </div>

              {/* Description Field */}
              <div>
                <label
                  htmlFor="description"
                  className="block text-sm font-medium text-gray-700 dark:text-gray-300"
                >
                  Description
                </label>
                <textarea
                  id="description"
                  name="description"
                  value={formData.description}
                  onChange={handleInputChange}
                  rows={2}
                  className="create-budget-dialog__input"
                  placeholder="What is this budget for?"
                />
              </div>

              {/* Period Field */}
              <div>
                <label
                  htmlFor="period"
                  className="block text-sm font-medium text-gray-700 dark:text-gray-300"
                >
                  Period
                </label>
                <select
                  id="period"
                  name="period"
                  value={formData.period}
                  onChange={handleInputChange}
                  className="create-budget-dialog__input"
                >
                  {PERIOD_OPTIONS.map((option) => (
                    <option key={option.value} value={option.value}>
                      {option.label}
                    </option>
                  ))}
                </select>
              </div>

              {/* Amount Field */}
              <div>
                <label
                  htmlFor="amount"
                  className="block text-sm font-medium text-gray-700 dark:text-gray-300"
                >
                  Amount
                </label>
                <div className="relative">
                  <span className="absolute left-3 top-1/2 -translate-y-1/2 text-gray-500">
                    $
                  </span>
                  <input
                    type="number"
                    id="amount"
                    name="amount"
                    value={formData.amount}
                    onChange={handleInputChange}
                    min="0"
                    step="0.01"
                    className={`create-budget-dialog__input pl-7 ${
                      errors.amount ? 'create-budget-dialog__input--error' : ''
                    }`}
                    placeholder="0.00"
                  />
                </div>
                {errors.amount && (
                  <p className="mt-1 text-sm text-red-600 dark:text-red-400">
                    {errors.amount}
                  </p>
                )}
              </div>

              {/* Start Date Field */}
              <div>
                <label
                  htmlFor="start_date"
                  className="block text-sm font-medium text-gray-700 dark:text-gray-300"
                >
                  Start Date
                </label>
                <input
                  type="date"
                  id="start_date"
                  name="start_date"
                  value={formData.start_date}
                  onChange={handleInputChange}
                  className="create-budget-dialog__input"
                />
              </div>

              {/* End Date Field (only for custom period) */}
              {formData.period === 'custom' && (
                <div>
                  <label
                    htmlFor="end_date"
                    className="block text-sm font-medium text-gray-700 dark:text-gray-300"
                  >
                    End Date
                  </label>
                  <input
                    type="date"
                    id="end_date"
                    name="end_date"
                    value={formData.end_date}
                    onChange={handleInputChange}
                    min={formData.start_date}
                    className={`create-budget-dialog__input ${
                      errors.end_date ? 'create-budget-dialog__input--error' : ''
                    }`}
                  />
                  {errors.end_date && (
                    <p className="mt-1 text-sm text-red-600 dark:text-red-400">
                      {errors.end_date}
                    </p>
                  )}
                </div>
              )}
            </TabsContent>

            {/* Issue #496: Income Tab */}
            <TabsContent value="income" className="space-y-4">
              <p className="text-sm text-gray-600 dark:text-gray-400">
                Configure income sources to track expected vs actual income for this budget.
              </p>
              <IncomeSourceConfig
                sources={formData.income_sources}
                onChange={handleIncomeSourcesChange}
                availableCategories={plaidCategories}
                availableTags={userTags}
                disabled={isSubmitting}
                showTotal
              />
              {/* Issue #496: Track actual income checkbox */}
              <label className="flex items-center gap-2 text-sm text-gray-700 dark:text-gray-300 cursor-pointer">
                <input
                  type="checkbox"
                  checked={formData.track_actual_income}
                  onChange={(e) =>
                    setFormData((prev) => ({ ...prev, track_actual_income: e.target.checked }))
                  }
                  disabled={isSubmitting}
                  className="h-4 w-4 rounded border-gray-300 text-blue-600 focus:ring-blue-500 dark:border-gray-600 dark:bg-gray-800"
                />
                Track actual income vs expected
              </label>
            </TabsContent>
          </Tabs>

          {/* API Error */}
          {apiError && (
            <div className="p-3 rounded-lg bg-red-50 dark:bg-red-900/30 text-red-600 dark:text-red-400 text-sm">
              {apiError}
            </div>
          )}

          {/* Actions */}
          <div className="flex justify-end gap-3 pt-4">
            <button
              type="button"
              onClick={onCancel}
              className="px-4 py-2 text-sm font-medium text-gray-700 dark:text-gray-200 bg-gray-100 dark:bg-gray-800 rounded-lg hover:bg-gray-200 dark:hover:bg-gray-700 transition-colors"
            >
              Cancel
            </button>
            <button
              type="submit"
              disabled={isSubmitting}
              className="px-4 py-2 text-sm font-medium text-white bg-blue-600 rounded-lg hover:bg-blue-700 disabled:opacity-50 disabled:cursor-not-allowed transition-colors"
            >
              {isSubmitting ? 'Creating...' : 'Create Budget'}
            </button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}
