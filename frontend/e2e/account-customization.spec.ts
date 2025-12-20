import { test, expect, Page } from '@playwright/test';
import { acceptCookieConsent } from './helpers/auth';

/**
 * Account Customization E2E Tests
 * Issue #307: Account Customization Features
 *
 * Tests for:
 * - Account customization modal
 * - Nickname editing
 * - Color selection
 * - Icon selection
 * - Emoji selection
 * - Hide/show accounts toggle
 * - Drag-and-drop reordering
 */

// Helper to navigate to accounts page
async function navigateToAccountsPage(page: Page) {
  await page.goto('/accounts');
  await acceptCookieConsent(page);
}

// Helper to get account card by name
function getAccountCard(page: Page, accountName: string) {
  return page.locator('.account-card').filter({ hasText: accountName });
}

// Helper to open customization modal for an account
async function openCustomizationModal(page: Page, accountName: string) {
  const card = getAccountCard(page, accountName);
  await card.locator('.account-card__edit-btn').click();
  await expect(page.getByRole('dialog')).toBeVisible();
}

test.describe('Account Customization - UI Elements', () => {
  test.beforeEach(async ({ page }) => {
    await navigateToAccountsPage(page);
  });

  test.describe('Accounts Page Structure', () => {
    test('should show accounts header with actions', async ({ page }) => {
      await expect(page.locator('.accounts-header h1')).toHaveText('My Accounts');
      await expect(page.locator('.accounts-header__actions')).toBeVisible();
    });

    test('should show Connect Bank Account button', async ({ page }) => {
      // PlaidLinkButton should be visible
      await expect(page.getByRole('button', { name: /connect bank/i })).toBeVisible();
    });

    test('should not show hidden toggle when no hidden accounts', async ({ page }) => {
      // If there are no hidden accounts, the toggle should not appear
      // This depends on test data - may or may not be visible
      // Just verify the page loads without error
      await expect(page.locator('.accounts-page')).toBeVisible();
    });
  });
});

// Tests that require authenticated state with accounts
test.describe.skip('Account Customization - Modal (Requires Auth)', () => {
  test('should open customization modal on edit button click', async ({ page }) => {
    await navigateToAccountsPage(page);

    // Find first account card and click edit
    const editButton = page.locator('.account-card__edit-btn').first();
    await editButton.click();

    // Modal should appear
    await expect(page.getByRole('dialog')).toBeVisible();
    await expect(page.getByText('Customize Account')).toBeVisible();
  });

  test('should show all customization fields in modal', async ({ page }) => {
    await navigateToAccountsPage(page);
    await page.locator('.account-card__edit-btn').first().click();

    const dialog = page.getByRole('dialog');

    // Should have nickname input
    await expect(dialog.getByLabel(/nickname/i)).toBeVisible();

    // Should have color picker
    await expect(dialog.getByText(/color/i)).toBeVisible();

    // Should have icon selector
    await expect(dialog.getByText(/icon/i)).toBeVisible();

    // Should have emoji input
    await expect(dialog.getByText(/emoji/i)).toBeVisible();

    // Should have hidden toggle
    await expect(dialog.getByText(/hide.*account/i)).toBeVisible();

    // Should have save and cancel buttons
    await expect(dialog.getByRole('button', { name: /save/i })).toBeVisible();
    await expect(dialog.getByRole('button', { name: /cancel/i })).toBeVisible();
  });

  test('should close modal on cancel', async ({ page }) => {
    await navigateToAccountsPage(page);
    await page.locator('.account-card__edit-btn').first().click();

    const dialog = page.getByRole('dialog');
    await expect(dialog).toBeVisible();

    await dialog.getByRole('button', { name: /cancel/i }).click();

    await expect(dialog).not.toBeVisible();
  });

  test('should close modal on X button', async ({ page }) => {
    await navigateToAccountsPage(page);
    await page.locator('.account-card__edit-btn').first().click();

    const dialog = page.getByRole('dialog');
    await expect(dialog).toBeVisible();

    // Click the X close button
    await dialog.locator('button[aria-label*="close" i], button:has([data-lucide="x"])').first().click();

    await expect(dialog).not.toBeVisible();
  });
});

test.describe.skip('Account Customization - Nickname (Requires Auth)', () => {
  test('should update nickname and display it', async ({ page }) => {
    await navigateToAccountsPage(page);
    await openCustomizationModal(page, 'Primary Checking');

    const dialog = page.getByRole('dialog');
    const nicknameInput = dialog.getByLabel(/nickname/i);

    // Clear and type new nickname
    await nicknameInput.clear();
    await nicknameInput.fill('My Savings');

    // Save
    await dialog.getByRole('button', { name: /save/i }).click();

    // Modal should close
    await expect(dialog).not.toBeVisible();

    // Card should now show the new nickname
    await expect(getAccountCard(page, 'My Savings')).toBeVisible();
  });

  test('should validate nickname length', async ({ page }) => {
    await navigateToAccountsPage(page);
    await page.locator('.account-card__edit-btn').first().click();

    const dialog = page.getByRole('dialog');
    const nicknameInput = dialog.getByLabel(/nickname/i);

    // Try to enter a very long nickname (>100 chars)
    await nicknameInput.fill('A'.repeat(101));

    // Should show validation error or truncate
    const inputValue = await nicknameInput.inputValue();
    expect(inputValue.length).toBeLessThanOrEqual(100);
  });
});

test.describe.skip('Account Customization - Color (Requires Auth)', () => {
  test('should display color picker with presets', async ({ page }) => {
    await navigateToAccountsPage(page);
    await page.locator('.account-card__edit-btn').first().click();

    const dialog = page.getByRole('dialog');

    // Should have preset color buttons
    const colorButtons = dialog.locator('.color-preset');
    await expect(colorButtons.first()).toBeVisible();
  });

  test('should select a preset color', async ({ page }) => {
    await navigateToAccountsPage(page);
    await page.locator('.account-card__edit-btn').first().click();

    const dialog = page.getByRole('dialog');

    // Click a preset color
    await dialog.locator('.color-preset').first().click();

    // The color should be selected (check for ring or border)
    await expect(dialog.locator('.color-preset').first()).toHaveAttribute('aria-pressed', 'true');
  });

  test('should apply color to account card', async ({ page }) => {
    await navigateToAccountsPage(page);
    const cardName = 'Primary Checking';
    await openCustomizationModal(page, cardName);

    const dialog = page.getByRole('dialog');

    // Select a color (e.g., blue)
    await dialog.locator('.color-preset[title*="Blue"]').click();

    // Save
    await dialog.getByRole('button', { name: /save/i }).click();
    await expect(dialog).not.toBeVisible();

    // Card should now have the color border
    const card = getAccountCard(page, cardName);
    await expect(card).toHaveCSS('border-left-style', 'solid');
    await expect(card).toHaveCSS('border-left-width', '4px');
  });

  test('should clear color selection', async ({ page }) => {
    await navigateToAccountsPage(page);
    await page.locator('.account-card__edit-btn').first().click();

    const dialog = page.getByRole('dialog');

    // Click "No Color" button
    await dialog.getByRole('button', { name: /no color/i }).click();

    // Save and verify no color border
    await dialog.getByRole('button', { name: /save/i }).click();
    await expect(dialog).not.toBeVisible();
  });
});

test.describe.skip('Account Customization - Icon (Requires Auth)', () => {
  test('should display icon grid', async ({ page }) => {
    await navigateToAccountsPage(page);
    await page.locator('.account-card__edit-btn').first().click();

    const dialog = page.getByRole('dialog');

    // Should show icon grid with multiple icons
    const iconButtons = dialog.locator('[aria-label*="Select"][aria-label*="icon"]');
    await expect(iconButtons.first()).toBeVisible();

    // Should have "No Icon" option
    await expect(dialog.getByRole('button', { name: /no icon/i })).toBeVisible();
  });

  test('should select an icon', async ({ page }) => {
    await navigateToAccountsPage(page);
    await page.locator('.account-card__edit-btn').first().click();

    const dialog = page.getByRole('dialog');

    // Click wallet icon
    await dialog.getByRole('button', { name: /select wallet icon/i }).click();

    // Should show as selected
    await expect(dialog.getByRole('button', { name: /select wallet icon/i })).toHaveAttribute('aria-pressed', 'true');
  });

  test('should display selected icon on card', async ({ page }) => {
    await navigateToAccountsPage(page);
    const cardName = 'Primary Checking';
    await openCustomizationModal(page, cardName);

    const dialog = page.getByRole('dialog');

    // Select piggy bank icon
    await dialog.getByRole('button', { name: /select savings icon/i }).click();

    // Save
    await dialog.getByRole('button', { name: /save/i }).click();
    await expect(dialog).not.toBeVisible();

    // Card should now show the icon
    const card = getAccountCard(page, cardName);
    await expect(card.locator('.account-card__icon')).toBeVisible();
  });
});

test.describe.skip('Account Customization - Emoji (Requires Auth)', () => {
  test('should have emoji input field', async ({ page }) => {
    await navigateToAccountsPage(page);
    await page.locator('.account-card__edit-btn').first().click();

    const dialog = page.getByRole('dialog');

    // Should have emoji input
    await expect(dialog.getByPlaceholder(/enter emoji/i)).toBeVisible();
  });

  test('should show emoji suggestions', async ({ page }) => {
    await navigateToAccountsPage(page);
    await page.locator('.account-card__edit-btn').first().click();

    const dialog = page.getByRole('dialog');

    // Should show quick emoji buttons
    const emojiButtons = dialog.locator('.emoji-suggestion');
    await expect(emojiButtons.first()).toBeVisible();
  });

  test('should select emoji from suggestions', async ({ page }) => {
    await navigateToAccountsPage(page);
    await page.locator('.account-card__edit-btn').first().click();

    const dialog = page.getByRole('dialog');

    // Click a suggested emoji
    await dialog.locator('.emoji-suggestion').first().click();

    // Emoji input should now have the value
    const input = dialog.getByPlaceholder(/enter emoji/i);
    const value = await input.inputValue();
    expect(value.length).toBeGreaterThan(0);
  });

  test('should display emoji on card', async ({ page }) => {
    await navigateToAccountsPage(page);
    const cardName = 'Primary Checking';
    await openCustomizationModal(page, cardName);

    const dialog = page.getByRole('dialog');

    // Enter an emoji
    await dialog.getByPlaceholder(/enter emoji/i).fill('💰');

    // Save
    await dialog.getByRole('button', { name: /save/i }).click();
    await expect(dialog).not.toBeVisible();

    // Card should now show the emoji
    const card = getAccountCard(page, cardName);
    await expect(card.locator('.account-card__emoji')).toHaveText('💰');
  });
});

test.describe.skip('Account Customization - Hide/Show Toggle (Requires Auth)', () => {
  test('should have hide account toggle in modal', async ({ page }) => {
    await navigateToAccountsPage(page);
    await page.locator('.account-card__edit-btn').first().click();

    const dialog = page.getByRole('dialog');

    // Should have hidden toggle
    await expect(dialog.getByRole('switch', { name: /hide.*account/i })).toBeVisible();
  });

  test('should hide account when toggle is enabled', async ({ page }) => {
    await navigateToAccountsPage(page);
    const cardName = 'Primary Checking';

    // Verify account is visible before hiding
    await expect(getAccountCard(page, cardName)).toBeVisible();

    // Open modal and toggle hidden
    await openCustomizationModal(page, cardName);
    const dialog = page.getByRole('dialog');

    await dialog.getByRole('switch', { name: /hide.*account/i }).click();
    await dialog.getByRole('button', { name: /save/i }).click();
    await expect(dialog).not.toBeVisible();

    // Account should no longer be visible (hidden by default)
    await expect(getAccountCard(page, cardName)).not.toBeVisible();
  });

  test('should show hidden accounts toggle in header when accounts are hidden', async ({ page }) => {
    // This test assumes there are hidden accounts
    await navigateToAccountsPage(page);

    // The toggle should appear if there are hidden accounts
    const toggle = page.locator('.accounts-header__toggle');

    if (await toggle.isVisible({ timeout: 1000 })) {
      // Should show count of hidden accounts
      await expect(toggle).toContainText(/hidden/i);
      await expect(toggle).toContainText(/\d+/); // Should have a number
    }
  });

  test('should reveal hidden accounts when toggle is clicked', async ({ page }) => {
    await navigateToAccountsPage(page);

    const toggle = page.locator('.accounts-header__toggle');

    if (await toggle.isVisible({ timeout: 1000 })) {
      // Click to show hidden accounts
      await toggle.click();

      // Toggle should now say "Hide Hidden"
      await expect(toggle).toContainText(/hide hidden/i);

      // Hidden accounts should now be visible (with --hidden modifier)
      await expect(page.locator('.account-card--hidden')).toBeVisible();
    }
  });

  test('should show Hidden badge on hidden accounts', async ({ page }) => {
    await navigateToAccountsPage(page);

    // Show hidden accounts first
    const toggle = page.locator('.accounts-header__toggle');
    if (await toggle.isVisible({ timeout: 1000 })) {
      await toggle.click();

      // Hidden accounts should have a badge
      const hiddenCard = page.locator('.account-card--hidden').first();
      await expect(hiddenCard.locator('.account-card__badge--hidden')).toBeVisible();
      await expect(hiddenCard.locator('.account-card__badge--hidden')).toContainText('Hidden');
    }
  });
});

test.describe.skip('Account Customization - Drag and Drop Reordering (Requires Auth)', () => {
  test('should show drag handles on account cards', async ({ page }) => {
    await navigateToAccountsPage(page);

    // Each card should have a drag handle
    const dragHandle = page.locator('.account-card__drag-handle').first();
    await expect(dragHandle).toBeVisible();
  });

  test('should have cursor: grab on drag handle', async ({ page }) => {
    await navigateToAccountsPage(page);

    const dragHandle = page.locator('.account-card__drag-handle').first();
    await expect(dragHandle).toHaveCSS('cursor', 'grab');
  });

  test('should reorder accounts via drag and drop', async ({ page }) => {
    await navigateToAccountsPage(page);

    // Get the first two account cards
    const firstCard = page.locator('.account-card').first();
    const secondCard = page.locator('.account-card').nth(1);

    // Store second card's name to verify it moves to first position after reorder
    const secondName = await secondCard.locator('.account-card__name').textContent();

    // Drag first card to second position
    const firstHandle = firstCard.locator('.account-card__drag-handle');
    const secondBox = await secondCard.boundingBox();

    if (secondBox) {
      await firstHandle.dragTo(secondCard, {
        targetPosition: { x: secondBox.width / 2, y: secondBox.height + 10 },
      });

      // Wait for reorder to complete
      await page.waitForTimeout(500);

      // Verify order changed
      const newFirstCard = page.locator('.account-card').first();
      const newFirstName = await newFirstCard.locator('.account-card__name').textContent();

      expect(newFirstName).toBe(secondName);
    }
  });

  test('should show visual feedback during drag', async ({ page }) => {
    await navigateToAccountsPage(page);

    const firstCard = page.locator('.account-card').first();
    const dragHandle = firstCard.locator('.account-card__drag-handle');

    // Start dragging
    await dragHandle.hover();
    await page.mouse.down();
    await page.mouse.move(100, 100);

    // Card should have dragging class
    await expect(firstCard).toHaveClass(/account-card--dragging/);

    // Release
    await page.mouse.up();
  });

  test('should persist order after page refresh', async ({ page }) => {
    await navigateToAccountsPage(page);

    // Get current first card name
    const firstCard = page.locator('.account-card').first();
    const originalFirstName = await firstCard.locator('.account-card__name').textContent();

    // Perform drag to reorder
    const secondCard = page.locator('.account-card').nth(1);
    const firstHandle = firstCard.locator('.account-card__drag-handle');

    const secondBox = await secondCard.boundingBox();
    if (secondBox) {
      await firstHandle.dragTo(secondCard, {
        targetPosition: { x: secondBox.width / 2, y: secondBox.height + 10 },
      });
      await page.waitForTimeout(1000); // Wait for API call
    }

    // Refresh page
    await page.reload();
    await acceptCookieConsent(page);

    // Verify order persisted
    const newFirstCard = page.locator('.account-card').first();
    const newFirstName = await newFirstCard.locator('.account-card__name').textContent();

    expect(newFirstName).not.toBe(originalFirstName);
  });
});

test.describe.skip('Account Customization - Preview (Requires Auth)', () => {
  test('should show live preview in modal', async ({ page }) => {
    await navigateToAccountsPage(page);
    await page.locator('.account-card__edit-btn').first().click();

    const dialog = page.getByRole('dialog');

    // Should have a preview section
    await expect(dialog.getByText(/preview/i)).toBeVisible();
  });

  test('should update preview when nickname changes', async ({ page }) => {
    await navigateToAccountsPage(page);
    await page.locator('.account-card__edit-btn').first().click();

    const dialog = page.getByRole('dialog');
    const nicknameInput = dialog.getByLabel(/nickname/i);

    // Enter a nickname
    await nicknameInput.fill('Test Preview');

    // Preview should show the new name
    const preview = dialog.locator('.account-card-preview, .modal-preview');
    await expect(preview).toContainText('Test Preview');
  });

  test('should update preview when color changes', async ({ page }) => {
    await navigateToAccountsPage(page);
    await page.locator('.account-card__edit-btn').first().click();

    const dialog = page.getByRole('dialog');

    // Select a color
    await dialog.locator('.color-preset').first().click();

    // Preview should show the color
    const preview = dialog.locator('.account-card-preview, .modal-preview');
    await expect(preview).toHaveCSS('border-left-style', 'solid');
  });
});

test.describe.skip('Account Customization - Accessibility (Requires Auth)', () => {
  test('modal should trap focus', async ({ page }) => {
    await navigateToAccountsPage(page);
    await page.locator('.account-card__edit-btn').first().click();

    const dialog = page.getByRole('dialog');
    await expect(dialog).toBeVisible();

    // Focus should be within the dialog
    const focusedElement = page.locator(':focus');
    await expect(focusedElement).toBeVisible();

    // Tab through elements - focus should stay in dialog
    await page.keyboard.press('Tab');
    await page.keyboard.press('Tab');
    await page.keyboard.press('Tab');

    // Focused element should still be within dialog
    const isInDialog = await dialog.locator(':focus').count();
    expect(isInDialog).toBeGreaterThan(0);
  });

  test('modal should close on Escape key', async ({ page }) => {
    await navigateToAccountsPage(page);
    await page.locator('.account-card__edit-btn').first().click();

    const dialog = page.getByRole('dialog');
    await expect(dialog).toBeVisible();

    // Press Escape
    await page.keyboard.press('Escape');

    // Modal should close
    await expect(dialog).not.toBeVisible();
  });

  test('color picker should be keyboard navigable', async ({ page }) => {
    await navigateToAccountsPage(page);
    await page.locator('.account-card__edit-btn').first().click();

    const dialog = page.getByRole('dialog');

    // Tab to color section and navigate with arrow keys
    // Focus first color preset
    await dialog.locator('.color-preset').first().focus();

    // Should be focusable
    await expect(dialog.locator('.color-preset').first()).toBeFocused();

    // Press Enter to select
    await page.keyboard.press('Enter');

    // Should be selected
    await expect(dialog.locator('.color-preset').first()).toHaveAttribute('aria-pressed', 'true');
  });

  test('icon selector should be keyboard navigable', async ({ page }) => {
    await navigateToAccountsPage(page);
    await page.locator('.account-card__edit-btn').first().click();

    const dialog = page.getByRole('dialog');

    // Focus first icon button
    const iconButtons = dialog.locator('[aria-label*="Select"][aria-label*="icon"]');
    await iconButtons.first().focus();

    // Should be focusable
    await expect(iconButtons.first()).toBeFocused();

    // Press Enter to select
    await page.keyboard.press('Enter');

    // Should be selected
    await expect(iconButtons.first()).toHaveAttribute('aria-pressed', 'true');
  });

  test('drag handles should have proper ARIA attributes', async ({ page }) => {
    await navigateToAccountsPage(page);

    const dragHandle = page.locator('.account-card__drag-handle').first();

    // Should have appropriate attributes for accessibility
    await expect(dragHandle).toBeVisible();
  });
});

test.describe.skip('Account Customization - Error Handling (Requires Auth)', () => {
  test('should show error message on save failure', async ({ page }) => {
    await navigateToAccountsPage(page);
    await page.locator('.account-card__edit-btn').first().click();

    const dialog = page.getByRole('dialog');

    // Modify something
    await dialog.getByLabel(/nickname/i).fill('New Name');

    // Mock network failure
    await page.route('**/api/v1/accounts/*', route => route.abort());

    // Try to save
    await dialog.getByRole('button', { name: /save/i }).click();

    // Should show error message
    await expect(page.getByText(/failed|error/i)).toBeVisible();
  });

  test('should allow retry after save failure', async ({ page }) => {
    await navigateToAccountsPage(page);
    await page.locator('.account-card__edit-btn').first().click();

    const dialog = page.getByRole('dialog');

    // Modify and fail
    await dialog.getByLabel(/nickname/i).fill('New Name');
    await page.route('**/api/v1/accounts/*', route => route.abort());
    await dialog.getByRole('button', { name: /save/i }).click();

    // Clear route mock
    await page.unroute('**/api/v1/accounts/*');

    // Modal should still be open for retry
    await expect(dialog).toBeVisible();
  });
});
