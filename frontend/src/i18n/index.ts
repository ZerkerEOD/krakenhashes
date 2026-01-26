import i18n from 'i18next';
import { initReactI18next } from 'react-i18next';
import LanguageDetector from 'i18next-browser-languagedetector';
import HttpBackend from 'i18next-http-backend';

/**
 * Supported languages configuration
 * Community can add new languages by:
 * 1. Adding an entry here
 * 2. Creating translation files in public/locales/{code}/
 */
export const supportedLanguages = {
    en: { nativeName: 'English', flag: '🇺🇸' },
    zh: { nativeName: '中文', flag: '🇨🇳' },
    de: { nativeName: 'Deutsch', flag: '🇩🇪' },
    nl: { nativeName: 'Nederlands', flag: '🇳🇱' },
    es: { nativeName: 'Español', flag: '🇪🇸' },
    ru: { nativeName: 'Русский', flag: '🇷🇺' },
} as const;

export type SupportedLanguage = keyof typeof supportedLanguages;

/**
 * Translation namespaces - enables lazy loading per feature area
 */
export const defaultNS = 'common';
export const namespaces = [
    'common', // Shared buttons, labels, pagination
    'navigation', // Menu items, sidebar
    'auth', // Login, MFA, authentication
    'dashboard', // Dashboard page
    'jobs', // Jobs management
    'agents', // Agent management
    'hashlists', // Hashlist management
    'pot', // Cracked hashes (Pot)
    'analytics', // Analytics reports
    'admin', // Admin settings
    'settings', // User settings
    'errors', // Error messages
] as const;

export type Namespace = (typeof namespaces)[number];

i18n
    // Load translations from /public/locales at runtime
    .use(HttpBackend)
    // Detect user language from browser/localStorage
    .use(LanguageDetector)
    // Pass i18n instance to react-i18next
    .use(initReactI18next)
    .init({
        // Fallback to English when translation is missing
        fallbackLng: 'en',

        // Debug mode only in development
        debug: process.env.NODE_ENV === 'development',

        // Namespaces configuration
        ns: [...namespaces],
        defaultNS,

        // Don't escape values - React handles this
        interpolation: {
            escapeValue: false,
        },

        // Backend configuration for loading translation files
        backend: {
            loadPath: '/locales/{{lng}}/{{ns}}.json',
        },

        // Language detection configuration
        detection: {
            // Order of detection: localStorage first, then browser
            order: ['localStorage', 'navigator', 'htmlTag'],
            // Cache user language choice in localStorage
            caches: ['localStorage'],
            // Key for localStorage
            lookupLocalStorage: 'krakenhashes_language',
        },

        // React-specific options
        react: {
            useSuspense: true,
            bindI18n: 'languageChanged',
            bindI18nStore: '',
            transEmptyNodeValue: '',
            transSupportBasicHtmlNodes: true,
            transKeepBasicHtmlNodesFor: ['br', 'strong', 'em', 'i', 'b'],
        },

        // Missing key handling - log warnings in development
        saveMissing: process.env.NODE_ENV === 'development',
        missingKeyHandler: (lngs, ns, key) => {
            if (process.env.NODE_ENV === 'development') {
                console.warn(
                    `[i18n] Missing translation key: ${ns}:${key} for languages: ${lngs.join(', ')}`
                );
            }
        },
    });

export default i18n;
