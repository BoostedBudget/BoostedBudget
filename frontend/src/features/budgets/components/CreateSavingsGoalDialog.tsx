/**
 * CreateSavingsGoalDialog
 * Issue #111: Goals Dashboard & Progress Visualization
 * Issue #462: Migrated from HeadlessUI to shadcn/ui
 *
 * Modal dialog for creating savings goals with template selection.
 * Uses shadcn/ui Dialog built on Radix UI.
 */

import { useState, useEffect, useCallback } from 'react';
import { XIcon } from 'lucide-react';
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { createSavingsGoal } from '../api/budgetsApi';
import { fetchAccounts } from '../../accounts/api/plaidApi';
import { GOAL_TEMPLATES, calculateDeadlineFromTemplate } from '../goalTemplates';
import type { GoalTemplate } from '../goalTemplates';
import type { Account } from '../../accounts/types';
import type { SavingsGoal, CreateSavingsGoalRequest } from '../types';
import './CreateSavingsGoalDialog.css';

export interface CreateSavingsGoalDialogProps {
  isOpen: boolean;
  onCancel: () => void;
  onGoalCreated: (goal: SavingsGoal) => void;
}

interface FormData {
  name: string;
  description: string;
  target_amount: string;
  account_id: string;
  deadline: string;
}

interface FormErrors {
  name?: string;
  target_amount?: string;
  account_id?: string;
}

const initialFormData: FormData = {
  name: '',
  description: '',
  target_amount: '',
  account_id: '',
  deadline: '',
};

export function CreateSavingsGoalDialog({
  isOpen,
  onCancel,
  onGoalCreated,
}: CreateSavingsGoalDialogProps) {
  const [formData, setFormData] = useState<FormData>(initialFormData);
  const [errors, setErrors] = useState<FormErrors>({});
  const [apiError, setApiError] = useState<string | null>(null);
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [accounts, setAccounts] = useState<Account[]>([]);
  const [accountsLoading, setAccountsLoading] = useState(false);
  const [selectedTemplate, setSelectedTemplate] = useState<string | null>(null);

  // Load accounts when dialog opens
  useEffect(() => {
    if (isOpen) {
      setAccountsLoading(true);
      fetchAccounts()
        .then((response) => {
          // Filter to depository accounts only
          const depositoryAccounts = (response.accounts ?? []).filter(
            (acc) => acc.type === 'depository'
          );
          setAccounts(depositoryAccounts);
        })
        .catch((err) => {
          console.error('Failed to fetch accounts:', err);
          setAccounts([]);
        })
        .finally(() => {
          setAccountsLoading(false);
        });
    }
  }, [isOpen]);

  // Reset form when dialog closes
  useEffect(() => {
    if (!isOpen) {
      setFormData(initialFormData);
      setErrors({});
      setApiError(null);
      setSelectedTemplate(null);
    }
  }, [isOpen]);

  const handleTemplateSelect = useCallback((template: GoalTemplate) => {
    setSelectedTemplate(template.id);
    setFormData((prev) => ({
      ...prev,
      name: template.id !== 'custom' ? template.name : prev.name,
      deadline: calculateDeadlineFromTemplate(template) || prev.deadline,
    }));
  }, []);

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

    const amount = parseFloat(formData.target_amount);
    if (isNaN(amount) || amount <= 0) {
      newErrors.target_amount = 'Amount must be greater than 0';
    }

    if (!formData.account_id) {
      newErrors.account_id = 'Please select an account';
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
        const request: CreateSavingsGoalRequest = {
          name: formData.name.trim(),
          description: formData.description.trim() || undefined,
          target_amount: parseFloat(formData.target_amount),
          account_id: formData.account_id,
          deadline: formData.deadline || undefined,
        };

        const newGoal = await createSavingsGoal(request);
        onGoalCreated(newGoal);
      } catch (err) {
        setApiError(err instanceof Error ? err.message : 'Failed to create goal');
      } finally {
        setIsSubmitting(false);
      }
    },
    [formData, validate, onGoalCreated]
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
        className="create-goal-dialog w-full max-w-lg p-6 bg-white dark:bg-gray-900 rounded-2xl"
        // Issue #460: Prevent click-outside-to-close to avoid accidental data loss
        onInteractOutside={(e) => e.preventDefault()}
        onPointerDownOutside={(e) => e.preventDefault()}
        showCloseButton={false}
      >
        <DialogHeader className="flex flex-row items-center justify-between mb-6">
          <DialogTitle className="text-xl font-semibold text-gray-900 dark:text-white">
            Create Savings Goal
          </DialogTitle>
          <button
            type="button"
            onClick={onCancel}
            className="text-gray-400 hover:text-gray-600 dark:hover:text-gray-300"
          >
            <XIcon className="h-6 w-6" />
          </button>
        </DialogHeader>

        {/* Template Selection */}
        <div className="mb-6">
          <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
            Choose a template (optional)
          </label>
          <div className="grid grid-cols-2 gap-2 sm:grid-cols-3">
            {GOAL_TEMPLATES.map((template) => (
              <button
                key={template.id}
                type="button"
                onClick={() => handleTemplateSelect(template)}
                className={`create-goal-dialog__template ${
                  selectedTemplate === template.id
                    ? 'create-goal-dialog__template--selected'
                    : ''
                }`}
              >
                <span className="text-sm font-medium">{template.name}</span>
              </button>
            ))}
          </div>
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
              className={`create-goal-dialog__input ${
                errors.name ? 'create-goal-dialog__input--error' : ''
              }`}
              placeholder="e.g., Vacation Fund"
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
              className="create-goal-dialog__input"
              placeholder="What are you saving for?"
            />
          </div>

          {/* Target Amount Field */}
          <div>
            <label
              htmlFor="target_amount"
              className="block text-sm font-medium text-gray-700 dark:text-gray-300"
            >
              Target Amount
            </label>
            <div className="relative">
              <span className="absolute left-3 top-1/2 -translate-y-1/2 text-gray-500">
                $
              </span>
              <input
                type="number"
                id="target_amount"
                name="target_amount"
                value={formData.target_amount}
                onChange={handleInputChange}
                min="0"
                step="0.01"
                className={`create-goal-dialog__input pl-7 ${
                  errors.target_amount ? 'create-goal-dialog__input--error' : ''
                }`}
                placeholder="0.00"
              />
            </div>
            {errors.target_amount && (
              <p className="mt-1 text-sm text-red-600 dark:text-red-400">
                {errors.target_amount}
              </p>
            )}
          </div>

          {/* Account Selector Field */}
          <div>
            <label
              htmlFor="account_id"
              className="block text-sm font-medium text-gray-700 dark:text-gray-300"
            >
              Account
            </label>
            <select
              id="account_id"
              name="account_id"
              value={formData.account_id}
              onChange={handleInputChange}
              className={`create-goal-dialog__input ${
                errors.account_id ? 'create-goal-dialog__input--error' : ''
              }`}
              disabled={accountsLoading}
            >
              <option value="">
                {accountsLoading ? 'Loading accounts...' : 'Select an account'}
              </option>
              {accounts.map((account) => (
                <option key={account.id} value={account.id}>
                  {account.name}
                  {account.mask ? ` (••••${account.mask})` : ''}
                  {account.institution_name ? ` - ${account.institution_name}` : ''}
                </option>
              ))}
            </select>
            {errors.account_id && (
              <p className="mt-1 text-sm text-red-600 dark:text-red-400">
                {errors.account_id}
              </p>
            )}
            {accounts.length === 0 && !accountsLoading && (
              <p className="mt-1 text-sm text-amber-600 dark:text-amber-400">
                No savings accounts found. Link an account first.
              </p>
            )}
          </div>

          {/* Deadline Field */}
          <div>
            <label
              htmlFor="deadline"
              className="block text-sm font-medium text-gray-700 dark:text-gray-300"
            >
              Deadline (optional)
            </label>
            <input
              type="date"
              id="deadline"
              name="deadline"
              value={formData.deadline}
              onChange={handleInputChange}
              className="create-goal-dialog__input"
              min={new Date().toISOString().split('T')[0]}
            />
          </div>

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
              {isSubmitting ? 'Creating...' : 'Create Goal'}
            </button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}
