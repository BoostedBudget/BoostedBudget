/**
 * Transaction Editing E2E Tests
 *
 * Tests the user flow for editing transaction tags and categories.
 * This critical flow was added to catch integration gaps between
 * components and the main page.
 *
 * Issue #368: Wire TransactionEditor into UI
 */

import { test, expect, type Page } from '@playwright/test'
import { loginWithClerk, acceptCookieConsent } from './helpers/auth'

// Mock data for transactions
const mockTransactions = {
  transactions: [
    {
      id: 'txn-1',
      user_id: 'user-123',
      account_id: 'acc-1',
      linked_item_id: 'item-1',
      source: 'plaid',
      external_transaction_id: 'plaid-txn-1',
      amount: -25.50,
      date: '2025-11-15',
      name: 'Coffee Shop',
      merchant_name: 'Starbucks',
      category: '["Food and Drink", "Coffee"]',
      pending: false,
      tags: ['Food'],
      created_at: '2025-11-15T10:00:00Z',
      updated_at: '2025-11-15T10:00:00Z',
    },
    {
      id: 'txn-2',
      user_id: 'user-123',
      account_id: 'acc-1',
      linked_item_id: 'item-1',
      source: 'plaid',
      external_transaction_id: 'plaid-txn-2',
      amount: -150.00,
      date: '2025-11-14',
      name: 'Grocery Store',
      merchant_name: 'Whole Foods',
      category: '["Shopping", "Groceries"]',
      pending: false,
      tags: [],
      created_at: '2025-11-14T10:00:00Z',
      updated_at: '2025-11-14T10:00:00Z',
    },
  ],
  total_count: 2,
  limit: 50,
  offset: 0,
  has_more: false,
}

const mockTags = [
  {
    id: 'tag-1',
    user_id: 'user-123',
    name: 'Food',
    color: '#FF5733',
    usage_count: 5,
    created_at: '2025-01-01T00:00:00Z',
    updated_at: '2025-01-01T00:00:00Z',
  },
  {
    id: 'tag-2',
    user_id: 'user-123',
    name: 'Shopping',
    color: '#33FF57',
    usage_count: 3,
    created_at: '2025-01-01T00:00:00Z',
    updated_at: '2025-01-01T00:00:00Z',
  },
  {
    id: 'tag-3',
    user_id: 'user-123',
    name: 'Entertainment',
    color: '#3357FF',
    usage_count: 2,
    created_at: '2025-01-01T00:00:00Z',
    updated_at: '2025-01-01T00:00:00Z',
  },
]

const mockCategories = [
  {
    id: 'cat-1',
    user_id: 'user-123',
    name: 'Food & Dining',
    color: '#FF5733',
    usage_count: 10,
    created_at: '2025-01-01T00:00:00Z',
    updated_at: '2025-01-01T00:00:00Z',
  },
  {
    id: 'cat-2',
    user_id: 'user-123',
    name: 'Shopping',
    color: '#33FF57',
    usage_count: 5,
    created_at: '2025-01-01T00:00:00Z',
    updated_at: '2025-01-01T00:00:00Z',
  },
  {
    id: 'cat-3',
    user_id: 'user-123',
    name: 'Entertainment',
    color: '#3357FF',
    usage_count: 3,
    created_at: '2025-01-01T00:00:00Z',
    updated_at: '2025-01-01T00:00:00Z',
  },
]

/**
 * Helper function to mock transaction editing APIs
 */
async function mockTransactionAPIs(
  page: Page,
  overrides: {
    transactions?: typeof mockTransactions
    tags?: typeof mockTags
    categories?: typeof mockCategories
    transactionsStatus?: number
    tagsStatus?: number
    categoriesStatus?: number
  } = {}
) {
  const {
    transactions = mockTransactions,
    tags = mockTags,
    categories = mockCategories,
    transactionsStatus = 200,
    tagsStatus = 200,
    categoriesStatus = 200,
  } = overrides

  // Mock transactions endpoint
  await page.route('**/api/v1/transactions**', (route) => {
    if (route.request().method() === 'GET') {
      route.fulfill({
        status: transactionsStatus,
        contentType: 'application/json',
        body: JSON.stringify(transactions),
      })
    } else {
      route.continue()
    }
  })

  // Mock tags endpoint (GET and POST)
  await page.route('**/api/v1/tags', (route) => {
    if (route.request().method() === 'GET') {
      route.fulfill({
        status: tagsStatus,
        contentType: 'application/json',
        body: JSON.stringify(tags),
      })
    } else if (route.request().method() === 'POST') {
      // Handle tag creation
      const body = route.request().postDataJSON()
      const newTag = {
        id: 'new-tag-id',
        user_id: 'user-123',
        name: body.name,
        color: body.color || '#6B7280',
        usage_count: 0,
        created_at: new Date().toISOString(),
        updated_at: new Date().toISOString(),
      }
      route.fulfill({
        status: 201,
        contentType: 'application/json',
        body: JSON.stringify(newTag),
      })
    } else {
      route.continue()
    }
  })

  // Mock categories endpoint (GET and POST)
  await page.route('**/api/v1/categories', (route) => {
    if (route.request().method() === 'GET') {
      route.fulfill({
        status: categoriesStatus,
        contentType: 'application/json',
        body: JSON.stringify(categories),
      })
    } else if (route.request().method() === 'POST') {
      // Handle category creation
      const body = route.request().postDataJSON()
      const newCategory = {
        id: 'new-cat-id',
        user_id: 'user-123',
        name: body.name,
        color: body.color || '#6B7280',
        usage_count: 0,
        created_at: new Date().toISOString(),
        updated_at: new Date().toISOString(),
      }
      route.fulfill({
        status: 201,
        contentType: 'application/json',
        body: JSON.stringify(newCategory),
      })
    } else {
      route.continue()
    }
  })
}

/**
 * Helper function to mock transaction update APIs
 */
async function mockTransactionUpdateAPIs(
  page: Page,
  options: {
    updateTagsStatus?: number
    updateCategoryStatus?: number
  } = {}
) {
  const { updateTagsStatus = 200, updateCategoryStatus = 200 } = options

  // Mock update tags endpoint
  await page.route('**/api/v1/transactions/*/tags', (route) => {
    if (route.request().method() === 'PUT') {
      route.fulfill({
        status: updateTagsStatus,
        contentType: 'application/json',
        body: JSON.stringify(mockTransactions.transactions[0]),
      })
    } else {
      route.continue()
    }
  })

  // Mock update category endpoint
  await page.route('**/api/v1/transactions/*/category', (route) => {
    if (route.request().method() === 'PUT') {
      route.fulfill({
        status: updateCategoryStatus,
        contentType: 'application/json',
        body: JSON.stringify(mockTransactions.transactions[0]),
      })
    } else {
      route.continue()
    }
  })
}

// ============================================================================
// Non-Authenticated Tests
// ============================================================================

test.describe('Transaction Editing - Protected Route', () => {
  test.beforeEach(async ({ context }) => {
    await context.clearCookies()
  })

  test('should redirect unauthenticated users away from transactions page', async ({
    page,
  }) => {
    await page.goto('/transactions')

    // Wait for redirect to complete (to home or Auth0)
    // Using Playwright's auto-retry assertion to handle redirect timing
    await expect(page).not.toHaveURL(/\/transactions/, { timeout: 10000 })
  })
})

// ============================================================================
// Authenticated Tests
// ============================================================================

const skipClerkTests = !process.env.VITE_CLERK_PUBLISHABLE_KEY
const describeTransactionEditing = skipClerkTests
  ? test.describe.skip
  : test.describe

describeTransactionEditing('Transaction Editing - Authenticated User', () => {
  test.beforeEach(async ({ page, context }) => {
    await context.clearCookies()

    const credentials = {
      email: process.env.TEST_CLERK_EMAIL || '',
      password: process.env.TEST_CLERK_PASSWORD || '',
    }

    await loginWithClerk(page, credentials)
    await acceptCookieConsent(page)
  })

  // ==========================================================================
  // Opening Editor Tests
  // ==========================================================================

  test.describe('Opening Editor', () => {
    test('should open editor modal when clicking a transaction', async ({
      page,
    }) => {
      // Arrange
      await mockTransactionAPIs(page)
      await page.goto('/transactions')
      await page.waitForLoadState('networkidle')

      // Wait for transactions to load
      await expect(page.getByText('Starbucks')).toBeVisible()

      // Act - Click on the transaction
      const transactionCard = page.getByText('Starbucks').locator('xpath=ancestor::article')
      await transactionCard.click()

      // Assert - Editor modal should open
      await expect(page.getByText('Edit Transaction')).toBeVisible()
    })

    test('should display transaction details in modal', async ({ page }) => {
      // Arrange
      await mockTransactionAPIs(page)
      await page.goto('/transactions')
      await page.waitForLoadState('networkidle')

      // Act
      const transactionCard = page.getByText('Starbucks').locator('xpath=ancestor::article')
      await transactionCard.click()

      // Assert
      await expect(page.getByText('Edit Transaction')).toBeVisible()
      await expect(page.getByText('$25.50')).toBeVisible()
    })

    test('should show current tags on transaction', async ({ page }) => {
      // Arrange
      await mockTransactionAPIs(page)
      await page.goto('/transactions')
      await page.waitForLoadState('networkidle')

      // Act - Click transaction that has a tag
      const transactionCard = page.getByText('Starbucks').locator('xpath=ancestor::article')
      await transactionCard.click()

      // Assert - Should show the Food tag
      await expect(page.getByText('Edit Transaction')).toBeVisible()
      // The tag should be visible in the editor
      const tagSection = page.locator('.tag-editor, [data-testid="tag-editor"]')
      await expect(tagSection.getByText('Food')).toBeVisible()
    })

    test('should show current category on transaction', async ({ page }) => {
      // Arrange
      await mockTransactionAPIs(page)
      await page.goto('/transactions')
      await page.waitForLoadState('networkidle')

      // Act
      const transactionCard = page.getByText('Starbucks').locator('xpath=ancestor::article')
      await transactionCard.click()

      // Assert - Should show category
      await expect(page.getByText('Edit Transaction')).toBeVisible()
      await expect(page.getByText('Food and Drink')).toBeVisible()
    })
  })

  // ==========================================================================
  // Tag Editing Tests
  // ==========================================================================

  test.describe('Tag Editing', () => {
    test('should add a tag to transaction', async ({ page }) => {
      // Arrange
      await mockTransactionAPIs(page)
      await mockTransactionUpdateAPIs(page)
      await page.goto('/transactions')
      await page.waitForLoadState('networkidle')

      // Open editor
      const transactionCard = page.getByText('Starbucks').locator('xpath=ancestor::article')
      await transactionCard.click()
      await expect(page.getByText('Edit Transaction')).toBeVisible()

      // Act - Click add tag button
      const addTagButton = page.getByRole('button', { name: /add tag/i })
      await addTagButton.click()

      // Select a tag from dropdown
      await expect(page.getByRole('option', { name: 'Shopping' })).toBeVisible()
      await page.getByRole('option', { name: 'Shopping' }).click()

      // Assert - Tag should appear in the list
      const tagEditor = page.locator('.tag-editor, [data-testid="tag-editor"]')
      await expect(tagEditor.getByText('Shopping')).toBeVisible()
    })

    test('should remove a tag from transaction', async ({ page }) => {
      // Arrange
      await mockTransactionAPIs(page)
      await mockTransactionUpdateAPIs(page)
      await page.goto('/transactions')
      await page.waitForLoadState('networkidle')

      // Open editor
      const transactionCard = page.getByText('Starbucks').locator('xpath=ancestor::article')
      await transactionCard.click()
      await expect(page.getByText('Edit Transaction')).toBeVisible()

      // Act - Remove the Food tag
      const removeButton = page.getByRole('button', { name: /remove food/i })
      await removeButton.click()

      // Assert - Tag should be removed
      const tagEditor = page.locator('.tag-editor, [data-testid="tag-editor"]')
      await expect(tagEditor.getByText('Food')).not.toBeVisible()
    })

    test('should persist tag changes after save', async ({ page }) => {
      // Arrange
      await mockTransactionAPIs(page)
      await mockTransactionUpdateAPIs(page)
      await page.goto('/transactions')
      await page.waitForLoadState('networkidle')

      // Open editor and remove a tag
      const transactionCard = page.getByText('Starbucks').locator('xpath=ancestor::article')
      await transactionCard.click()

      const removeButton = page.getByRole('button', { name: /remove food/i })
      await removeButton.click()

      // Act - Click save
      const saveButton = page.getByRole('button', { name: /save/i })
      await saveButton.click()

      // Assert - Modal should close
      await expect(page.getByText('Edit Transaction')).not.toBeVisible()
    })
  })

  // ==========================================================================
  // Category Editing Tests
  // ==========================================================================

  test.describe('Category Editing', () => {
    test('should change transaction category', async ({ page }) => {
      // Arrange
      await mockTransactionAPIs(page)
      await mockTransactionUpdateAPIs(page)
      await page.goto('/transactions')
      await page.waitForLoadState('networkidle')

      // Open editor
      const transactionCard = page.getByText('Starbucks').locator('xpath=ancestor::article')
      await transactionCard.click()
      await expect(page.getByText('Edit Transaction')).toBeVisible()

      // Act - Click change category button
      const changeCategoryButton = page.getByRole('button', { name: /change category/i })
      await changeCategoryButton.click()

      // Select a different category
      await expect(page.getByRole('option', { name: 'Shopping' })).toBeVisible()
      await page.getByRole('option', { name: 'Shopping' }).click()

      // Assert - New category should be selected
      await expect(page.getByText('Shopping')).toBeVisible()
    })

    test('should clear transaction category', async ({ page }) => {
      // Arrange
      await mockTransactionAPIs(page)
      await mockTransactionUpdateAPIs(page)
      await page.goto('/transactions')
      await page.waitForLoadState('networkidle')

      // Open editor
      const transactionCard = page.getByText('Starbucks').locator('xpath=ancestor::article')
      await transactionCard.click()
      await expect(page.getByText('Edit Transaction')).toBeVisible()

      // Act - Click clear category button
      const clearButton = page.getByRole('button', { name: /clear category/i })
      await clearButton.click()

      // Assert - Should show uncategorized
      await expect(page.getByText(/uncategorized/i)).toBeVisible()
    })

    test('should persist category changes after save', async ({ page }) => {
      // Arrange
      await mockTransactionAPIs(page)
      await mockTransactionUpdateAPIs(page)
      await page.goto('/transactions')
      await page.waitForLoadState('networkidle')

      // Open editor and change category
      const transactionCard = page.getByText('Starbucks').locator('xpath=ancestor::article')
      await transactionCard.click()

      const changeCategoryButton = page.getByRole('button', { name: /change category/i })
      await changeCategoryButton.click()
      await page.getByRole('option', { name: 'Entertainment' }).click()

      // Act - Click save
      const saveButton = page.getByRole('button', { name: /save/i })
      await saveButton.click()

      // Assert - Modal should close
      await expect(page.getByText('Edit Transaction')).not.toBeVisible()
    })
  })

  // ==========================================================================
  // Save/Cancel Tests
  // ==========================================================================

  test.describe('Save and Cancel', () => {
    test('should close modal when cancel clicked', async ({ page }) => {
      // Arrange
      await mockTransactionAPIs(page)
      await page.goto('/transactions')
      await page.waitForLoadState('networkidle')

      // Open editor
      const transactionCard = page.getByText('Starbucks').locator('xpath=ancestor::article')
      await transactionCard.click()
      await expect(page.getByText('Edit Transaction')).toBeVisible()

      // Act - Click cancel
      const cancelButton = page.getByRole('button', { name: /cancel/i })
      await cancelButton.click()

      // Assert
      await expect(page.getByText('Edit Transaction')).not.toBeVisible()
    })

    test('should show error on save failure', async ({ page }) => {
      // Arrange
      await mockTransactionAPIs(page)
      await mockTransactionUpdateAPIs(page, { updateTagsStatus: 500 })
      await page.goto('/transactions')
      await page.waitForLoadState('networkidle')

      // Open editor and make a change
      const transactionCard = page.getByText('Starbucks').locator('xpath=ancestor::article')
      await transactionCard.click()

      const removeButton = page.getByRole('button', { name: /remove food/i })
      await removeButton.click()

      // Act - Click save
      const saveButton = page.getByRole('button', { name: /save/i })
      await saveButton.click()

      // Assert - Error message should appear
      await expect(page.getByText(/failed to save/i)).toBeVisible()
    })
  })

  // ==========================================================================
  // Edge Cases Tests
  // ==========================================================================

  test.describe('Edge Cases', () => {
    test('should handle transaction with no tags', async ({ page }) => {
      // Arrange
      await mockTransactionAPIs(page)
      await page.goto('/transactions')
      await page.waitForLoadState('networkidle')

      // Act - Click transaction without tags (Whole Foods)
      const transactionCard = page.getByText('Whole Foods').locator('xpath=ancestor::article')
      await transactionCard.click()

      // Assert - Should show empty tags state
      await expect(page.getByText('Edit Transaction')).toBeVisible()
      await expect(page.getByText(/no tags/i)).toBeVisible()
    })

    test('should make transactions keyboard accessible', async ({ page }) => {
      // Arrange
      await mockTransactionAPIs(page)
      await page.goto('/transactions')
      await page.waitForLoadState('networkidle')

      // Wait for transactions to load
      await expect(page.getByText('Starbucks')).toBeVisible()

      // Act - Focus on transaction and press Enter
      const transactionCard = page.getByText('Starbucks').locator('xpath=ancestor::article')
      await transactionCard.focus()
      await transactionCard.press('Enter')

      // Assert - Editor should open
      await expect(page.getByText('Edit Transaction')).toBeVisible()
    })
  })

  // ==========================================================================
  // Inline Tag Creation Tests
  // ==========================================================================

  test.describe('Inline Tag Creation', () => {
    test('should show create new tag option in dropdown', async ({ page }) => {
      // Arrange
      await mockTransactionAPIs(page)
      await page.goto('/transactions')
      await page.waitForLoadState('networkidle')

      // Open editor
      const transactionCard = page.getByText('Starbucks').locator('xpath=ancestor::article')
      await transactionCard.click()
      await expect(page.getByText('Edit Transaction')).toBeVisible()

      // Act - Click add tag button
      const addTagButton = page.getByRole('button', { name: /add tag/i })
      await addTagButton.click()

      // Assert - Create new tag option should be visible
      await expect(page.getByText(/create new tag/i)).toBeVisible()
    })

    test('should show creation form when clicking create new tag', async ({ page }) => {
      // Arrange
      await mockTransactionAPIs(page)
      await page.goto('/transactions')
      await page.waitForLoadState('networkidle')

      // Open editor
      const transactionCard = page.getByText('Starbucks').locator('xpath=ancestor::article')
      await transactionCard.click()
      await expect(page.getByText('Edit Transaction')).toBeVisible()

      // Act - Click add tag then create new
      const addTagButton = page.getByRole('button', { name: /add tag/i })
      await addTagButton.click()
      await page.getByText(/create new tag/i).click()

      // Assert - Creation form should appear
      await expect(page.getByPlaceholder(/tag name/i)).toBeVisible()
      await expect(page.getByRole('button', { name: /^create$/i })).toBeVisible()
    })

    test('should create new tag and add to transaction', async ({ page }) => {
      // Arrange
      await mockTransactionAPIs(page)
      await mockTransactionUpdateAPIs(page)
      await page.goto('/transactions')
      await page.waitForLoadState('networkidle')

      // Open editor
      const transactionCard = page.getByText('Starbucks').locator('xpath=ancestor::article')
      await transactionCard.click()
      await expect(page.getByText('Edit Transaction')).toBeVisible()

      // Act - Create a new tag
      const addTagButton = page.getByRole('button', { name: /add tag/i })
      await addTagButton.click()
      await page.getByText(/create new tag/i).click()

      await page.getByPlaceholder(/tag name/i).fill('My New Tag')
      await page.getByRole('button', { name: /^create$/i }).click()

      // Assert - New tag should appear in the tag list
      const tagEditor = page.locator('.tag-editor, [data-testid="tag-editor"]')
      await expect(tagEditor.getByText('My New Tag')).toBeVisible()
    })

    test('should cancel tag creation and return to dropdown', async ({ page }) => {
      // Arrange
      await mockTransactionAPIs(page)
      await page.goto('/transactions')
      await page.waitForLoadState('networkidle')

      // Open editor
      const transactionCard = page.getByText('Starbucks').locator('xpath=ancestor::article')
      await transactionCard.click()
      await expect(page.getByText('Edit Transaction')).toBeVisible()

      // Act - Start creation then cancel
      const addTagButton = page.getByRole('button', { name: /add tag/i })
      await addTagButton.click()
      await page.getByText(/create new tag/i).click()
      await page.getByRole('button', { name: /cancel/i }).click()

      // Assert - Should be back to dropdown with existing tags
      await expect(page.getByRole('option', { name: 'Shopping' })).toBeVisible()
    })

    test('should allow creating tag when no tags exist', async ({ page }) => {
      // Arrange - Mock with empty tags
      await mockTransactionAPIs(page, { tags: [] })
      await page.goto('/transactions')
      await page.waitForLoadState('networkidle')

      // Open editor
      const transactionCard = page.getByText('Starbucks').locator('xpath=ancestor::article')
      await transactionCard.click()
      await expect(page.getByText('Edit Transaction')).toBeVisible()

      // Act - Click add tag (should be enabled even with no tags)
      const addTagButton = page.getByRole('button', { name: /add tag/i })
      await addTagButton.click()

      // Assert - Create option should be available
      await expect(page.getByText(/create new tag/i)).toBeVisible()
    })
  })

  // ==========================================================================
  // Inline Category Creation Tests
  // ==========================================================================

  test.describe('Inline Category Creation', () => {
    test('should show create new category option in dropdown', async ({ page }) => {
      // Arrange
      await mockTransactionAPIs(page)
      await page.goto('/transactions')
      await page.waitForLoadState('networkidle')

      // Open editor
      const transactionCard = page.getByText('Starbucks').locator('xpath=ancestor::article')
      await transactionCard.click()
      await expect(page.getByText('Edit Transaction')).toBeVisible()

      // Act - Click change category button
      const changeCategoryButton = page.getByRole('button', { name: /change category/i })
      await changeCategoryButton.click()

      // Assert - Create new category option should be visible
      await expect(page.getByText(/create new category/i)).toBeVisible()
    })

    test('should show creation form when clicking create new category', async ({ page }) => {
      // Arrange
      await mockTransactionAPIs(page)
      await page.goto('/transactions')
      await page.waitForLoadState('networkidle')

      // Open editor
      const transactionCard = page.getByText('Starbucks').locator('xpath=ancestor::article')
      await transactionCard.click()
      await expect(page.getByText('Edit Transaction')).toBeVisible()

      // Act - Click change category then create new
      const changeCategoryButton = page.getByRole('button', { name: /change category/i })
      await changeCategoryButton.click()
      await page.getByText(/create new category/i).click()

      // Assert - Creation form should appear
      await expect(page.getByPlaceholder(/category name/i)).toBeVisible()
      await expect(page.getByRole('button', { name: /^create$/i })).toBeVisible()
    })

    test('should create new category and assign to transaction', async ({ page }) => {
      // Arrange
      await mockTransactionAPIs(page)
      await mockTransactionUpdateAPIs(page)
      await page.goto('/transactions')
      await page.waitForLoadState('networkidle')

      // Open editor
      const transactionCard = page.getByText('Starbucks').locator('xpath=ancestor::article')
      await transactionCard.click()
      await expect(page.getByText('Edit Transaction')).toBeVisible()

      // Act - Create a new category
      const changeCategoryButton = page.getByRole('button', { name: /change category/i })
      await changeCategoryButton.click()
      await page.getByText(/create new category/i).click()

      await page.getByPlaceholder(/category name/i).fill('My New Category')
      await page.getByRole('button', { name: /^create$/i }).click()

      // Assert - New category should be selected
      await expect(page.getByText('My New Category')).toBeVisible()
    })

    test('should cancel category creation and return to dropdown', async ({ page }) => {
      // Arrange
      await mockTransactionAPIs(page)
      await page.goto('/transactions')
      await page.waitForLoadState('networkidle')

      // Open editor
      const transactionCard = page.getByText('Starbucks').locator('xpath=ancestor::article')
      await transactionCard.click()
      await expect(page.getByText('Edit Transaction')).toBeVisible()

      // Act - Start creation then cancel
      const changeCategoryButton = page.getByRole('button', { name: /change category/i })
      await changeCategoryButton.click()
      await page.getByText(/create new category/i).click()
      await page.getByRole('button', { name: /cancel/i }).click()

      // Assert - Should be back to dropdown with existing categories
      await expect(page.getByRole('option', { name: 'Entertainment' })).toBeVisible()
    })

    test('should allow creating category when no categories exist', async ({ page }) => {
      // Arrange - Mock with empty categories
      await mockTransactionAPIs(page, { categories: [] })
      await page.goto('/transactions')
      await page.waitForLoadState('networkidle')

      // Open editor
      const transactionCard = page.getByText('Starbucks').locator('xpath=ancestor::article')
      await transactionCard.click()
      await expect(page.getByText('Edit Transaction')).toBeVisible()

      // Act - Click change category
      const changeCategoryButton = page.getByRole('button', { name: /change category/i })
      await changeCategoryButton.click()

      // Assert - Create option should be available
      await expect(page.getByText(/create new category/i)).toBeVisible()
    })
  })

  // ==========================================================================
  // Save and Display Verification Tests (Issue #372)
  // ==========================================================================

  test.describe('Save and Display Verification', () => {
    test('should display saved tags on TransactionCard after save', async ({ page }) => {
      // Arrange - Set up mocks with updated transaction data after save
      let savedTags: string[] = ['Food']

      await page.route('**/api/v1/transactions**', (route) => {
        if (route.request().method() === 'GET') {
          // Return transactions with current saved tags
          const transactions = {
            ...mockTransactions,
            transactions: mockTransactions.transactions.map((txn, index) =>
              index === 0 ? { ...txn, tags: savedTags } : txn
            ),
          }
          route.fulfill({
            status: 200,
            contentType: 'application/json',
            body: JSON.stringify(transactions),
          })
        } else {
          route.continue()
        }
      })

      await page.route('**/api/v1/tags', (route) => {
        route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify(mockTags),
        })
      })

      await page.route('**/api/v1/categories', (route) => {
        route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify(mockCategories),
        })
      })

      // Mock the update endpoint to update savedTags
      await page.route('**/api/v1/transactions/*/tags', (route) => {
        if (route.request().method() === 'PUT') {
          const body = route.request().postDataJSON()
          savedTags = body.tags
          route.fulfill({
            status: 200,
            contentType: 'application/json',
            body: JSON.stringify({ ...mockTransactions.transactions[0], tags: savedTags }),
          })
        } else {
          route.continue()
        }
      })

      await page.goto('/transactions')
      await page.waitForLoadState('networkidle')

      // Verify initial tag displays
      const transactionCard = page.locator('article').first()
      await expect(transactionCard.getByText('Food')).toBeVisible()

      // Open editor and add a new tag
      await transactionCard.click()
      await expect(page.getByText('Edit Transaction')).toBeVisible()

      // Add Shopping tag
      await page.getByRole('button', { name: /add tag/i }).click()
      await page.getByRole('option', { name: 'Shopping' }).click()

      // Save
      await page.getByRole('button', { name: /save/i }).click()

      // Wait for modal to close and refetch
      await expect(page.getByText('Edit Transaction')).not.toBeVisible()

      // Assert - Both tags should now display on the card
      await expect(transactionCard.getByText('Food')).toBeVisible()
      await expect(transactionCard.getByText('Shopping')).toBeVisible()
    })

    test('should display saved category override on TransactionCard after save', async ({ page }) => {
      // Arrange - Set up mocks with category override after save
      let savedCategoryOverride: string | null = null

      await page.route('**/api/v1/transactions**', (route) => {
        if (route.request().method() === 'GET') {
          const transactions = {
            ...mockTransactions,
            transactions: mockTransactions.transactions.map((txn, index) =>
              index === 0
                ? { ...txn, category_override: savedCategoryOverride }
                : txn
            ),
          }
          route.fulfill({
            status: 200,
            contentType: 'application/json',
            body: JSON.stringify(transactions),
          })
        } else {
          route.continue()
        }
      })

      await page.route('**/api/v1/tags', (route) => {
        route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify(mockTags),
        })
      })

      await page.route('**/api/v1/categories', (route) => {
        route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify(mockCategories),
        })
      })

      // Mock the update endpoint to update savedCategoryOverride
      await page.route('**/api/v1/transactions/*/category', (route) => {
        if (route.request().method() === 'PUT') {
          const body = route.request().postDataJSON()
          savedCategoryOverride = JSON.stringify(body.category)
          route.fulfill({
            status: 200,
            contentType: 'application/json',
            body: JSON.stringify({
              ...mockTransactions.transactions[0],
              category_override: savedCategoryOverride
            }),
          })
        } else {
          route.continue()
        }
      })

      await page.goto('/transactions')
      await page.waitForLoadState('networkidle')

      // Verify initial category displays (original Plaid category)
      const transactionCard = page.locator('article').first()
      await expect(transactionCard.getByText('Food and Drink')).toBeVisible()

      // Open editor and change category
      await transactionCard.click()
      await expect(page.getByText('Edit Transaction')).toBeVisible()

      // Change to Shopping category
      await page.getByRole('button', { name: /change category/i }).click()
      await page.getByRole('option', { name: 'Shopping' }).click()

      // Save
      await page.getByRole('button', { name: /save/i }).click()

      // Wait for modal to close and refetch
      await expect(page.getByText('Edit Transaction')).not.toBeVisible()

      // Assert - New category should display instead of original
      await expect(transactionCard.getByText('Shopping')).toBeVisible()
      await expect(transactionCard.getByText('Food and Drink')).not.toBeVisible()
    })

    test('should display tags on TransactionCard for transaction with existing tags', async ({ page }) => {
      // Arrange
      await mockTransactionAPIs(page)
      await page.goto('/transactions')
      await page.waitForLoadState('networkidle')

      // Assert - First transaction should show its tag
      const firstTransaction = page.locator('article').first()
      await expect(firstTransaction.getByText('Food')).toBeVisible()
    })

    test('should not display tags section when transaction has no tags', async ({ page }) => {
      // Arrange
      await mockTransactionAPIs(page)
      await page.goto('/transactions')
      await page.waitForLoadState('networkidle')

      // Assert - Second transaction (Whole Foods) has empty tags array
      const secondTransaction = page.locator('article').nth(1)
      await expect(secondTransaction.getByText('Whole Foods')).toBeVisible()

      // Should not have any tags displayed
      const tagsContainer = secondTransaction.locator('[data-testid="transaction-tags"]')
      await expect(tagsContainer).not.toBeVisible()
    })
  })
})
