/**
 * MilestoneIndicator Tests
 * Issue #111: Goals Dashboard & Progress Visualization
 *
 * TDD tests for milestone markers on progress bars.
 */

import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MilestoneIndicator } from './MilestoneIndicator';

describe('MilestoneIndicator', () => {
  // ==========================================================================
  // Rendering Tests
  // ==========================================================================

  describe('Rendering', () => {
    it('renders milestone markers at correct positions', () => {
      render(<MilestoneIndicator progress={50} milestones={[25, 50, 75, 100]} />);

      // Should render 4 milestone markers
      const markers = screen.getAllByTestId(/milestone-marker-/);
      expect(markers).toHaveLength(4);
    });

    it('renders default milestones when not specified', () => {
      render(<MilestoneIndicator progress={30} />);

      // Default milestones are 25%, 50%, 75%, 100%
      const markers = screen.getAllByTestId(/milestone-marker-/);
      expect(markers).toHaveLength(4);
    });

    it('applies reached class to passed milestones', () => {
      render(<MilestoneIndicator progress={60} milestones={[25, 50, 75, 100]} />);

      // 25% and 50% should be reached
      expect(screen.getByTestId('milestone-marker-25')).toHaveClass('milestone-indicator__marker--reached');
      expect(screen.getByTestId('milestone-marker-50')).toHaveClass('milestone-indicator__marker--reached');
      // 75% and 100% should NOT be reached
      expect(screen.getByTestId('milestone-marker-75')).not.toHaveClass('milestone-indicator__marker--reached');
      expect(screen.getByTestId('milestone-marker-100')).not.toHaveClass('milestone-indicator__marker--reached');
    });

    it('applies next-milestone class to the next upcoming milestone', () => {
      render(<MilestoneIndicator progress={30} milestones={[25, 50, 75, 100]} />);

      // 50% is the next milestone
      expect(screen.getByTestId('milestone-marker-50')).toHaveClass('milestone-indicator__marker--next');
      // Others should not have this class
      expect(screen.getByTestId('milestone-marker-25')).not.toHaveClass('milestone-indicator__marker--next');
      expect(screen.getByTestId('milestone-marker-75')).not.toHaveClass('milestone-indicator__marker--next');
    });

    it('positions markers based on percentage', () => {
      render(<MilestoneIndicator progress={0} milestones={[25, 50, 75, 100]} />);

      const marker25 = screen.getByTestId('milestone-marker-25');
      const marker50 = screen.getByTestId('milestone-marker-50');
      const marker75 = screen.getByTestId('milestone-marker-75');
      const marker100 = screen.getByTestId('milestone-marker-100');

      // Check inline styles for positioning
      expect(marker25).toHaveStyle({ left: '25%' });
      expect(marker50).toHaveStyle({ left: '50%' });
      expect(marker75).toHaveStyle({ left: '75%' });
      expect(marker100).toHaveStyle({ left: '100%' });
    });
  });

  // ==========================================================================
  // Progress Display Tests
  // ==========================================================================

  describe('Progress Display', () => {
    it('shows percentage labels when showLabels is true', () => {
      render(<MilestoneIndicator progress={50} milestones={[25, 50, 75, 100]} showLabels />);

      expect(screen.getByText('25%')).toBeInTheDocument();
      expect(screen.getByText('50%')).toBeInTheDocument();
      expect(screen.getByText('75%')).toBeInTheDocument();
      expect(screen.getByText('100%')).toBeInTheDocument();
    });

    it('does not show labels by default', () => {
      render(<MilestoneIndicator progress={50} milestones={[25, 50, 75, 100]} />);

      expect(screen.queryByText('25%')).not.toBeInTheDocument();
      expect(screen.queryByText('50%')).not.toBeInTheDocument();
    });

    it('handles 0% progress correctly', () => {
      render(<MilestoneIndicator progress={0} milestones={[25, 50, 75, 100]} />);

      // No milestone should be reached
      const markers = screen.getAllByTestId(/milestone-marker-/);
      markers.forEach((marker) => {
        expect(marker).not.toHaveClass('milestone-indicator__marker--reached');
      });

      // 25% should be next
      expect(screen.getByTestId('milestone-marker-25')).toHaveClass('milestone-indicator__marker--next');
    });

    it('handles 100% progress correctly', () => {
      render(<MilestoneIndicator progress={100} milestones={[25, 50, 75, 100]} />);

      // All milestones should be reached
      const markers = screen.getAllByTestId(/milestone-marker-/);
      markers.forEach((marker) => {
        expect(marker).toHaveClass('milestone-indicator__marker--reached');
      });

      // No next milestone
      markers.forEach((marker) => {
        expect(marker).not.toHaveClass('milestone-indicator__marker--next');
      });
    });

    it('handles progress between milestones', () => {
      render(<MilestoneIndicator progress={37} milestones={[25, 50, 75, 100]} />);

      // 25% reached, 50% is next
      expect(screen.getByTestId('milestone-marker-25')).toHaveClass('milestone-indicator__marker--reached');
      expect(screen.getByTestId('milestone-marker-50')).toHaveClass('milestone-indicator__marker--next');
      expect(screen.getByTestId('milestone-marker-50')).not.toHaveClass('milestone-indicator__marker--reached');
    });
  });

  // ==========================================================================
  // Custom Milestones Tests
  // ==========================================================================

  describe('Custom Milestones', () => {
    it('renders custom milestone values', () => {
      render(<MilestoneIndicator progress={50} milestones={[10, 30, 60, 90]} />);

      expect(screen.getByTestId('milestone-marker-10')).toBeInTheDocument();
      expect(screen.getByTestId('milestone-marker-30')).toBeInTheDocument();
      expect(screen.getByTestId('milestone-marker-60')).toBeInTheDocument();
      expect(screen.getByTestId('milestone-marker-90')).toBeInTheDocument();
    });

    it('handles single milestone', () => {
      render(<MilestoneIndicator progress={80} milestones={[50]} />);

      const markers = screen.getAllByTestId(/milestone-marker-/);
      expect(markers).toHaveLength(1);
      expect(screen.getByTestId('milestone-marker-50')).toHaveClass('milestone-indicator__marker--reached');
    });

    it('handles empty milestones array', () => {
      render(<MilestoneIndicator progress={50} milestones={[]} />);

      // Should not render any markers
      const markers = screen.queryAllByTestId(/milestone-marker-/);
      expect(markers).toHaveLength(0);
    });
  });

  // ==========================================================================
  // Accessibility Tests
  // ==========================================================================

  describe('Accessibility', () => {
    it('has accessible role for markers', () => {
      render(<MilestoneIndicator progress={10} milestones={[25, 50, 75, 100]} />);

      // Each marker should have an accessible label (none reached at 10% progress)
      expect(screen.getByTestId('milestone-marker-25')).toHaveAttribute('aria-label', '25% milestone');
      expect(screen.getByTestId('milestone-marker-50')).toHaveAttribute('aria-label', '50% milestone');
    });

    it('indicates reached status in aria-label', () => {
      render(<MilestoneIndicator progress={60} milestones={[25, 50, 75]} />);

      expect(screen.getByTestId('milestone-marker-25')).toHaveAttribute('aria-label', '25% milestone (reached)');
      expect(screen.getByTestId('milestone-marker-50')).toHaveAttribute('aria-label', '50% milestone (reached)');
      expect(screen.getByTestId('milestone-marker-75')).toHaveAttribute('aria-label', '75% milestone');
    });
  });
});
