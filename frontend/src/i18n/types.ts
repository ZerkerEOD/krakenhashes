/**
 * TypeScript type definitions for i18next
 *
 * This file extends i18next's types to provide better TypeScript support.
 * Note: Full type safety for translation keys requires importing the JSON files,
 * which is not possible with the current tsconfig (include: ["src"]).
 *
 * For full type safety, you would need to either:
 * 1. Update tsconfig.json to include the public directory
 * 2. Move translation files to src/ (but i18next-http-backend expects public/)
 *
 * For now, this provides basic namespace typing without full key inference.
 */

import 'i18next';

// Define the namespace types
export type TranslationNamespace =
    | 'common'
    | 'navigation'
    | 'auth'
    | 'dashboard'
    | 'jobs'
    | 'agents'
    | 'hashlists'
    | 'pot'
    | 'analytics'
    | 'admin'
    | 'settings'
    | 'errors';

// Declare custom type options for i18next
declare module 'i18next' {
    interface CustomTypeOptions {
        defaultNS: 'common';
        // Allow any string key within namespaces
        // This provides namespace-level type safety without full key inference
        resources: Record<TranslationNamespace, Record<string, unknown>>;
    }
}
