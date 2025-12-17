/**
 * Tags Feature Module
 *
 * Exports all public APIs for the tags feature.
 */

// Types
export type { UserTag } from './types';

// API
export { fetchTags, createTag, updateTag, deleteTag } from './api/tagsApi';

// Hooks
export { useTags } from './hooks/useTags';
export type { UseTagsResult } from './hooks/useTags';
