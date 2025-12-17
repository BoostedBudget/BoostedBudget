/**
 * useTags Hook
 * Issue #497: Budget Category Manager + Match Config UI
 *
 * React hook for fetching user tags with loading and error states.
 */

import { useState, useEffect, useCallback } from 'react';
import { fetchTags } from '../api/tagsApi';
import type { UserTag } from '../types';

export interface UseTagsResult {
  /** List of user tags */
  tags: UserTag[];
  /** Whether the tags are currently loading */
  loading: boolean;
  /** Any error that occurred during fetching */
  error: Error | null;
  /** Function to refetch the tags */
  refetch: () => Promise<void>;
}

/**
 * Hook for fetching and managing user tags
 *
 * @returns Object containing tags, loading state, error, and refetch function
 *
 * @example
 * ```tsx
 * function TagSelector() {
 *   const { tags, loading, error, refetch } = useTags();
 *
 *   if (loading) return <LoadingSpinner />;
 *   if (error) return <ErrorMessage error={error} onRetry={refetch} />;
 *
 *   return (
 *     <select>
 *       {tags.map(tag => (
 *         <option key={tag.id} value={tag.name}>{tag.name}</option>
 *       ))}
 *     </select>
 *   );
 * }
 * ```
 */
export function useTags(): UseTagsResult {
  const [tags, setTags] = useState<UserTag[]>([]);
  const [loading, setLoading] = useState<boolean>(true);
  const [error, setError] = useState<Error | null>(null);

  // Load tags function
  const loadTags = useCallback(async () => {
    try {
      setLoading(true);
      setError(null);
      const result = await fetchTags();
      // Sort tags alphabetically by name
      const sortedTags = [...result].sort((a, b) => a.name.localeCompare(b.name));
      setTags(sortedTags);
    } catch (err) {
      setError(err instanceof Error ? err : new Error('Failed to fetch tags'));
      setTags([]);
    } finally {
      setLoading(false);
    }
  }, []);

  // Initial load
  useEffect(() => {
    loadTags();
  }, [loadTags]);

  // Refetch function
  const refetch = useCallback(async () => {
    await loadTags();
  }, [loadTags]);

  return {
    tags,
    loading,
    error,
    refetch,
  };
}
