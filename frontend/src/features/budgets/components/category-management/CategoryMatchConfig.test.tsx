/**
 * CategoryMatchConfig Tests
 * Issue #497: Budget Category Manager + Match Config UI
 *
 * TDD RED Phase: Write failing tests first
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { CategoryMatchConfig } from './CategoryMatchConfig';
import { MATCH_LOGIC_LABELS, MATCH_LOGIC_DESCRIPTIONS } from '@/shared/types/matching';

describe('CategoryMatchConfig', () => {
  const mockOnChange = vi.fn();

  beforeEach(() => {
    vi.clearAllMocks();
  });

  describe('Rendering', () => {
    it('should render all four match logic options as radio buttons', () => {
      render(<CategoryMatchConfig value="category_only" onChange={mockOnChange} />);

      // Should have 4 radio buttons
      const radios = screen.getAllByRole('radio');
      expect(radios).toHaveLength(4);
    });

    it('should show labels from MATCH_LOGIC_LABELS for each option', () => {
      render(<CategoryMatchConfig value="category_only" onChange={mockOnChange} />);

      // Check each label is present
      expect(screen.getByLabelText(MATCH_LOGIC_LABELS.category_only)).toBeInTheDocument();
      expect(screen.getByLabelText(MATCH_LOGIC_LABELS.tag_only)).toBeInTheDocument();
      expect(screen.getByLabelText(MATCH_LOGIC_LABELS.and)).toBeInTheDocument();
      expect(screen.getByLabelText(MATCH_LOGIC_LABELS.or)).toBeInTheDocument();
    });

    it('should show description text for each option', () => {
      render(<CategoryMatchConfig value="category_only" onChange={mockOnChange} />);

      // Check each description is present
      expect(screen.getByText(MATCH_LOGIC_DESCRIPTIONS.category_only)).toBeInTheDocument();
      expect(screen.getByText(MATCH_LOGIC_DESCRIPTIONS.tag_only)).toBeInTheDocument();
      expect(screen.getByText(MATCH_LOGIC_DESCRIPTIONS.and)).toBeInTheDocument();
      expect(screen.getByText(MATCH_LOGIC_DESCRIPTIONS.or)).toBeInTheDocument();
    });

    it('should pre-select the current value', () => {
      render(<CategoryMatchConfig value="tag_only" onChange={mockOnChange} />);

      const tagOnlyRadio = screen.getByLabelText(MATCH_LOGIC_LABELS.tag_only);
      expect(tagOnlyRadio).toBeChecked();

      const categoryOnlyRadio = screen.getByLabelText(MATCH_LOGIC_LABELS.category_only);
      expect(categoryOnlyRadio).not.toBeChecked();
    });

    it('should pre-select "and" when value is "and"', () => {
      render(<CategoryMatchConfig value="and" onChange={mockOnChange} />);

      const andRadio = screen.getByLabelText(MATCH_LOGIC_LABELS.and);
      expect(andRadio).toBeChecked();
    });

    it('should pre-select "or" when value is "or"', () => {
      render(<CategoryMatchConfig value="or" onChange={mockOnChange} />);

      const orRadio = screen.getByLabelText(MATCH_LOGIC_LABELS.or);
      expect(orRadio).toBeChecked();
    });
  });

  describe('Interactions', () => {
    it('should call onChange with new value when option selected', async () => {
      const user = userEvent.setup({ delay: null });
      render(<CategoryMatchConfig value="category_only" onChange={mockOnChange} />);

      const tagOnlyRadio = screen.getByLabelText(MATCH_LOGIC_LABELS.tag_only);
      await user.click(tagOnlyRadio);

      expect(mockOnChange).toHaveBeenCalledTimes(1);
      expect(mockOnChange).toHaveBeenCalledWith('tag_only');
    });

    it('should call onChange with "and" when AND option selected', async () => {
      const user = userEvent.setup({ delay: null });
      render(<CategoryMatchConfig value="category_only" onChange={mockOnChange} />);

      const andRadio = screen.getByLabelText(MATCH_LOGIC_LABELS.and);
      await user.click(andRadio);

      expect(mockOnChange).toHaveBeenCalledWith('and');
    });

    it('should call onChange with "or" when OR option selected', async () => {
      const user = userEvent.setup({ delay: null });
      render(<CategoryMatchConfig value="category_only" onChange={mockOnChange} />);

      const orRadio = screen.getByLabelText(MATCH_LOGIC_LABELS.or);
      await user.click(orRadio);

      expect(mockOnChange).toHaveBeenCalledWith('or');
    });

    it('should support keyboard navigation with arrow keys', async () => {
      const user = userEvent.setup({ delay: null });
      render(<CategoryMatchConfig value="category_only" onChange={mockOnChange} />);

      // Focus the first radio
      const categoryOnlyRadio = screen.getByLabelText(MATCH_LOGIC_LABELS.category_only);
      categoryOnlyRadio.focus();

      // Press down arrow to move to next option
      await user.keyboard('{ArrowDown}');

      // The native radio group should handle this - onChange should be called
      // Note: Browser native radio groups handle arrow key navigation
      expect(mockOnChange).toHaveBeenCalled();
    });
  });

  describe('Disabled State', () => {
    it('should disable all radio inputs when disabled=true', () => {
      render(
        <CategoryMatchConfig value="category_only" onChange={mockOnChange} disabled={true} />
      );

      const radios = screen.getAllByRole('radio');
      radios.forEach((radio) => {
        expect(radio).toBeDisabled();
      });
    });

    it('should not call onChange when disabled and clicked', async () => {
      const user = userEvent.setup({ delay: null });
      render(
        <CategoryMatchConfig value="category_only" onChange={mockOnChange} disabled={true} />
      );

      const tagOnlyRadio = screen.getByLabelText(MATCH_LOGIC_LABELS.tag_only);
      await user.click(tagOnlyRadio);

      expect(mockOnChange).not.toHaveBeenCalled();
    });
  });

  describe('Accessibility', () => {
    it('should wrap options in fieldset with legend', () => {
      render(<CategoryMatchConfig value="category_only" onChange={mockOnChange} />);

      // Should have a fieldset
      const fieldset = screen.getByRole('group');
      expect(fieldset).toBeInTheDocument();

      // Should have a legend with "Match Logic" text
      expect(screen.getByText('Match Logic')).toBeInTheDocument();
    });

    it('should have accessible descriptions linked via aria-describedby', () => {
      render(<CategoryMatchConfig value="category_only" onChange={mockOnChange} />);

      // Each radio should have an aria-describedby pointing to its description
      const categoryOnlyRadio = screen.getByLabelText(MATCH_LOGIC_LABELS.category_only);
      expect(categoryOnlyRadio).toHaveAttribute('aria-describedby');

      // The describedby should point to an element with the description text
      const describedById = categoryOnlyRadio.getAttribute('aria-describedby');
      if (describedById) {
        const descriptionElement = document.getElementById(describedById);
        expect(descriptionElement).toHaveTextContent(MATCH_LOGIC_DESCRIPTIONS.category_only);
      }
    });

    it('should have unique IDs for all radio inputs', () => {
      render(<CategoryMatchConfig value="category_only" onChange={mockOnChange} />);

      const radios = screen.getAllByRole('radio');
      const ids = radios.map((r) => r.id);

      // All IDs should be unique
      const uniqueIds = new Set(ids);
      expect(uniqueIds.size).toBe(4);
    });
  });
});
