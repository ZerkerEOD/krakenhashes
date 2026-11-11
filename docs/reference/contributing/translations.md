# Contributing Translations to KrakenHashes

Thank you for helping translate KrakenHashes! This guide explains how to add or improve translations for the application.

## Overview

KrakenHashes uses [react-i18next](https://react.i18next.com/) for internationalization. Translation files are stored as JSON in the `frontend/public/locales/` directory.

## Supported Languages

| Code | Language | Code | Language |
|---|---|---|---|
| `en` | English (reference) | `ja` | Japanese |
| `de` | German | `ko` | Korean |
| `es` | Spanish | `pt-BR` | Portuguese (Brazil) |
| `fr` | French | `it` | Italian |
| `nl` | Dutch | `pl` | Polish |
| `ru` | Russian | `tr` | Turkish |
| `zh` | Chinese (Simplified) | `uk` | Ukrainian |
| | | `cs` | Czech |
| | | `vi` | Vietnamese |
| | | `id` | Indonesian |

All languages other than English were machine-translated and then reviewed by a second model against a
per-language glossary. They are good but not native-quality: corrections from native speakers are very welcome
(see [Updating Existing Translations](#updating-existing-translations)).

Right-to-left languages (Arabic, Hebrew, Persian) are not supported yet; the layout would need mirroring first.

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
│   ├── notifications.json # Notification system
│   └── errors.json        # Error messages
├── _context/              # Translator notes for ambiguous keys (not loaded by the app)
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

Use [ISO 639-1 language codes](https://en.wikipedia.org/wiki/List_of_ISO_639-1_codes). Add a region only when
the variant matters (we use `pt-BR`; browsers asking for `pt` or `pt-PT` fall back to it):
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
  en: { nativeName: 'English', countryCode: 'US' },
  // Add your language here:
  YOUR_CODE: { nativeName: 'Native Name', countryCode: 'XX' },
};
```

Then:
- import the flag in `frontend/src/components/common/LanguageSelector.tsx` (`country-flag-icons/react/3x2`);
- add the DataGrid locale in `frontend/src/components/ui/DataTable/localeText.ts` and the MUI/date-fns locales in
  `frontend/src/i18n/locales.ts` if they exist for your language.

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

Plural strings use the CLDR category suffixes (i18next v21+). English has `_one` and `_other`:

```json
{
  "files_one": "{{count}} file",
  "files_other": "{{count}} files"
}
```

Each language provides the categories it needs, for example Polish `files_one`, `files_few`, `files_many`,
`files_other`, and Japanese only `files_other`. Run
`node -e "console.log(new Intl.PluralRules('pl').resolvedOptions().pluralCategories)"` to see the list; the
validator checks that all of them are present. See the
[i18next pluralization docs](https://www.i18next.com/translation-function/plurals).

### Glossary

Each language has a glossary in `frontend/scripts/i18n/glossary/<lang>.json` that fixes how domain terms
(hashlist, potfile, keyspace, agent, …) are rendered, and which stay in English. Follow it so the same concept is
named the same way everywhere. The source term list is `glossary/terms.json`.

### Context-Aware Translation

Consider the context where each string appears:
- Button labels should be concise
- Error messages should be helpful
- Navigation items should fit in the menu

## Validation and Tools

Scripts live in `frontend/scripts/i18n/` (plain Node, no install needed):

```bash
cd frontend
node scripts/i18n/validate.mjs pl              # check one language (or omit for all)
node scripts/i18n/validate.mjs --allow-missing # what CI runs
node scripts/i18n/chunk.mjs pl                 # list untranslated keys as work chunks
node scripts/i18n/merge.mjs pl result.json     # merge {"translations": {"ns:key": "…"}} into pl/
```

The validator fails on invalid JSON, empty strings, changed `{{placeholders}}` or markup tags, and missing plural
categories. It warns about text identical to English and strings much longer than the English (layout risk).

CI runs on every pull request that touches locale files:

1. **JSON syntax**: all files must be valid JSON
2. **Validator** (above, with `--allow-missing`): errors block the PR
3. **Missing keys**: warnings for keys present in English but missing in a translation
4. **Coverage report**: percentage translated per language

Partial translations never block a PR; broken ones do.

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
