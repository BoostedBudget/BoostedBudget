/**
 * useTags Hook
 * Issue #497: Budget Category Manager + Match Config UI
 *
 * React hook for fetching user tags.
 * Uses generated React Query hook from OpenAPI spec with built-in caching.
 */

import { useMemo } from 'react';
import { useGetUserTags } from '@boostedbudget/api-client/endpoints/tags/tags';
import type { UserTag } from '@boostedbudget/api-client/models';

export interface UseTagsResult {
  /** List of user tags (sorted alphabetically) */
  tags: UserTag[];
  /** Whether the tags are currently loading */
  loading: boolean;
  /** Any error that occurred during fetching */
  error: Error | null;
  /** Function to refetch the tags */
  refetch: () => void;
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
  const { data, isLoading, error, refetch: queryRefetch } = useGetUserTags({
    query: {
      staleTime: 5 * 60 * 1000, // 5 minutes cache
      gcTime: 5 * 60 * 1000,
    },
  });

  // Sort tags alphabetically by name
  const sortedTags = useMemo(() => {
    if (!data) return [];
    return [...data].sort((a, b) => a.name.localeCompare(b.name));
  }, [data]);

  return {
    tags: sortedTags,
    loading: isLoading,
    error: error ? (error instanceof Error ? error : new Error('Failed to fetch tags')) : null,
    refetch: queryRefetch,
  };
}
