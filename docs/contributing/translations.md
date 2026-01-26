# Contributing Translations to KrakenHashes

Thank you for helping translate KrakenHashes! This guide explains how to add or improve translations for the application.

## Overview

KrakenHashes uses [react-i18next](https://react.i18next.com/) for internationalization. Translation files are stored as JSON in the `frontend/public/locales/` directory.

## Directory Structure

```
frontend/public/locales/
├── en/                    # English (reference language)
│   ├── common.json        # Shared: buttons, labels, pagination
│   ├── navigation.json    # Menu items, sidebar
│   ├── auth.json          # Login, MFA, authentication
│   ├── dashboard.json     # Dashboard page
│   ├── jobs.json          # Jobs management
│   ├── agents.json        # Agent management
│   ├── hashlists.json     # Hashlist management
│   ├── pot.json           # Cracked hashes (Pot)
│   ├── analytics.json     # Analytics reports
│   ├── admin.json         # Admin settings
│   ├── settings.json      # User settings
│   └── errors.json        # Error messages
├── zh/                    # Chinese (example)
│   └── ... (same files)
├── de/                    # German (example)
│   └── ... (same files)
└── ...                    # Other languages
```

## Adding a New Language

### Step 1: Fork the Repository

1. Fork the KrakenHashes repository on GitHub
2. Clone your fork locally

### Step 2: Create the Language Directory

Copy the English locale folder to create your new language:

```bash
cd frontend/public/locales
cp -r en YOUR_LANG_CODE
```

Use [ISO 639-1 language codes](https://en.wikipedia.org/wiki/List_of_ISO_639-1_codes):
- `zh` - Chinese
- `de` - German
- `es` - Spanish
- `fr` - French
- `ja` - Japanese
- `ko` - Korean
- `pt` - Portuguese
- `ru` - Russian

### Step 3: Register the Language

Add your language to the supported languages in `frontend/src/i18n/index.ts`:

```typescript
export const supportedLanguages = {
  en: { nativeName: 'English', flag: '🇺🇸' },
  zh: { nativeName: '中文', flag: '🇨🇳' },
  // Add your language here:
  YOUR_CODE: { nativeName: 'Native Name', flag: '🏳️' },
};
```

### Step 4: Translate the Files

Translate each JSON file in your new language directory. Keep the following in mind:

1. **Keep placeholders intact**: `{{name}}`, `{{count}}`, etc. must remain unchanged
2. **Preserve HTML tags**: `<strong>`, `<em>` should stay in place
3. **Maintain key structure**: Do not rename or remove keys
4. **Use native language**: Write translations in the native language, not transliterated

Example:
```json
// English (en/common.json)
{
  "buttons": {
    "save": "Save",
    "cancel": "Cancel"
  },
  "pagination": {
    "showing": "Showing {{from}}-{{to}} of {{total}}"
  }
}

// Chinese (zh/common.json)
{
  "buttons": {
    "save": "保存",
    "cancel": "取消"
  },
  "pagination": {
    "showing": "显示 {{from}}-{{to}} / {{total}}"
  }
}
```

### Step 5: Test Your Translations

1. Install dependencies: `cd frontend && npm install`
2. Start the development server: `npm start`
3. Change language in the app using the language selector
4. Verify all translated text appears correctly

### Step 6: Submit a Pull Request

1. Commit your changes with a descriptive message:
   ```bash
   git add .
   git commit -m "feat(i18n): add Chinese translations"
   ```
2. Push to your fork
3. Create a Pull Request to the main repository

## Translation Guidelines

### Technical Terms

Some terms should remain in English:
- `hashcat` - The tool name
- `NTLM`, `MD5`, `SHA-1` - Hash algorithm names
- Technical identifiers and codes

### Placeholders

Always keep placeholders exactly as they appear in the English version:
- `{{count}}` - For pluralization
- `{{name}}` - For dynamic values
- `{{from}}`, `{{to}}`, `{{total}}` - For pagination

### Pluralization

i18next supports pluralization. Use `_plural` suffix for plural forms:

```json
{
  "items": "{{count}} item",
  "items_plural": "{{count}} items"
}
```

Some languages have multiple plural forms. See [i18next pluralization docs](https://www.i18next.com/translation-function/plurals).

### Context-Aware Translation

Consider the context where each string appears:
- Button labels should be concise
- Error messages should be helpful
- Navigation items should fit in the menu

## Validation

Our CI automatically validates translation files on Pull Requests:

1. **JSON syntax**: All files must be valid JSON
2. **Missing keys**: Warnings for keys present in English but missing in translations
3. **Coverage report**: Shows percentage of translated strings

These checks help maintain translation quality but won't block PRs with partial translations.

## Updating Existing Translations

If you notice incorrect or outdated translations:

1. Fork and clone the repository
2. Edit the appropriate JSON file
3. Test your changes
4. Submit a Pull Request with a clear description

## Getting Help

- Open an issue with the `translations` label for questions
- Check existing translation PRs for examples
- Refer to the English files as the authoritative source

## Recognition

Contributors who submit translations are recognized in the project's contributors list. Thank you for helping make KrakenHashes accessible to users worldwide!
