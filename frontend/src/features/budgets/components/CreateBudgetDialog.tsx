/**
 * CreateBudgetDialog
 * Issue #111: Goals Dashboard & Progress Visualization
 *
 * Modal dialog for creating budgets with period selection.
 * Uses Headless UI Dialog consistent with other dialogs.
 */

import { Fragment, useState, useEffect, useCallback } from 'react';
import { Dialog, Transition } from '@headlessui/react';
import { XMarkIcon } from '@heroicons/react/24/outline';
import { createBudget } from '../api/budgetsApi';
import type { Budget, BudgetPeriod, CreateBudgetRequest } from '../types';
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

  // Reset form when dialog closes
  useEffect(() => {
    if (!isOpen) {
      setFormData({ ...initialFormData, start_date: getDefaultStartDate() });
      setErrors({});
      setApiError(null);
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
          start_date: formData.start_date,
          end_date: formData.period === 'custom' ? formData.end_date : undefined,
          amount: parseFloat(formData.amount),
        };

        const newBudget = await createBudget(request);
        onBudgetCreated(newBudget);
      } catch (err) {
        setApiError(err instanceof Error ? err.message : 'Failed to create budget');
      } finally {
        setIsSubmitting(false);
      }
    },
    [formData, validate, onBudgetCreated]
  );

  return (
    <Transition appear show={isOpen} as={Fragment}>
      <Dialog as="div" className="relative z-50" onClose={onCancel}>
        <Transition.Child
          as={Fragment}
          enter="ease-out duration-300"
          enterFrom="opacity-0"
          enterTo="opacity-100"
          leave="ease-in duration-200"
          leaveFrom="opacity-100"
          leaveTo="opacity-0"
        >
          <div className="fixed inset-0 bg-black/25 dark:bg-black/50" />
        </Transition.Child>

        <div className="fixed inset-0 overflow-y-auto">
          <div className="flex min-h-full items-center justify-center p-4">
            <Transition.Child
              as={Fragment}
              enter="ease-out duration-300"
              enterFrom="opacity-0 scale-95"
              enterTo="opacity-100 scale-100"
              leave="ease-in duration-200"
              leaveFrom="opacity-100 scale-100"
              leaveTo="opacity-0 scale-95"
            >
              <Dialog.Panel className="create-budget-dialog w-full max-w-lg transform overflow-hidden rounded-2xl bg-white dark:bg-gray-900 p-6 shadow-xl transition-all">
                <div className="flex items-center justify-between mb-6">
                  <Dialog.Title
                    as="h2"
                    className="text-xl font-semibold text-gray-900 dark:text-white"
                  >
                    Create Budget
                  </Dialog.Title>
                  <button
                    type="button"
                    onClick={onCancel}
                    className="text-gray-400 hover:text-gray-600 dark:hover:text-gray-300"
                  >
                    <XMarkIcon className="h-6 w-6" />
                  </button>
                </div>

                <form onSubmit={handleSubmit} className="space-y-4">
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
              </Dialog.Panel>
            </Transition.Child>
          </div>
        </div>
      </Dialog>
    </Transition>
  );
}
