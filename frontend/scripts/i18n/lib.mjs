// Shared helpers for the translation scripts. Plain Node (>=18), no deps.
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

export const ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../..');
export const LOCALES = path.join(ROOT, 'public', 'locales');
export const CONTEXT_DIR = path.join(LOCALES, '_context');
export const SOURCE = 'en';

export const PLURAL_SUFFIXES = ['zero', 'one', 'two', 'few', 'many', 'other'];
const PLURAL_RE = new RegExp(`_(${PLURAL_SUFFIXES.join('|')})$`);

/** Locale folders, excluding helper folders that start with "_". */
export const languages = () =>
  fs.readdirSync(LOCALES).filter((d) => !d.startsWith('_') && fs.statSync(path.join(LOCALES, d)).isDirectory());

export const namespaces = () =>
  fs.readdirSync(path.join(LOCALES, SOURCE)).filter((f) => f.endsWith('.json')).map((f) => f.slice(0, -5)).sort();

export const nsPath = (lang, ns) => path.join(LOCALES, lang, `${ns}.json`);

export const readJson = (file) => JSON.parse(fs.readFileSync(file, 'utf8'));

export const loadNs = (lang, ns) => {
  const file = nsPath(lang, ns);
  return fs.existsSync(file) ? readJson(file) : {};
};

/** Nested object → ordered [dottedKey, value] pairs (insertion order kept). */
export const flatten = (obj, prefix = '', out = []) => {
  for (const [k, v] of Object.entries(obj)) {
    const key = prefix ? `${prefix}.${k}` : k;
    if (v && typeof v === 'object' && !Array.isArray(v)) flatten(v, key, out);
    else out.push([key, v]);
  }
  return out;
};

/** Ordered pairs → nested object. */
export const unflatten = (pairs) => {
  const root = {};
  for (const [key, value] of pairs) {
    const parts = key.split('.');
    let cur = root;
    for (let i = 0; i < parts.length - 1; i++) {
      if (typeof cur[parts[i]] !== 'object' || cur[parts[i]] === null) cur[parts[i]] = {};
      cur = cur[parts[i]];
    }
    cur[parts[parts.length - 1]] = value;
  }
  return root;
};

export const writeNs = (lang, ns, pairs) => {
  fs.mkdirSync(path.join(LOCALES, lang), { recursive: true });
  fs.writeFileSync(nsPath(lang, ns), JSON.stringify(unflatten(pairs), null, 2) + '\n');
};

export const pluralCategories = (lang) => new Intl.PluralRules(lang).resolvedOptions().pluralCategories;

export const pluralBase = (key) => {
  const m = key.match(PLURAL_RE);
  return m ? key.slice(0, -m[0].length) : null;
};

/**
 * The keys a language must have for one namespace, in English order. English
 * plural groups (`x_one` + `x_other`) expand to the language's own categories.
 * Returns [{key, base?, category?, en}] where `en` is the English text to show
 * the translator (for plural rows: the `_other` form, or `_one` for `one`).
 */
export const expectedKeys = (lang, ns) => {
  const en = flatten(loadNs(SOURCE, ns));
  const enMap = new Map(en);
  const groups = new Map();
  for (const [key] of en) {
    const base = pluralBase(key);
    if (base && (enMap.has(`${base}_one`) || enMap.has(`${base}_other`))) groups.set(base, true);
  }
  const cats = pluralCategories(lang);
  const out = [];
  const seen = new Set();
  for (const [key, value] of en) {
    const base = pluralBase(key);
    if (base && groups.has(base)) {
      if (seen.has(base)) continue;
      seen.add(base);
      for (const c of cats) {
        const enText = enMap.get(`${base}_${c}`) ?? enMap.get(c === 'one' ? `${base}_one` : `${base}_other`) ?? enMap.get(`${base}_other`);
        out.push({ key: `${base}_${c}`, base, category: c, en: enText });
      }
    } else {
      out.push({ key, en: value });
    }
  }
  return out;
};

/** Translator notes for a namespace: `_context/<ns>.json` plus any `_context/<ns>.<part>.json`. */
export const loadContext = (ns) => {
  const map = new Map();
  if (!fs.existsSync(CONTEXT_DIR)) return map;
  for (const f of fs.readdirSync(CONTEXT_DIR).sort()) {
    if (f === `${ns}.json` || (f.startsWith(`${ns}.`) && f.endsWith('.json'))) {
      for (const [k, v] of flatten(readJson(path.join(CONTEXT_DIR, f)))) map.set(k, v);
    }
  }
  return map;
};

/** Strings that legitimately stay the same in every language. */
export const SAME_OK = /^(?:[\d\s.,:%/+()\-–—]+|OK|ID|URL|API|IP|DNS|SMB|NFS|TLS|SSL|SSO|MFA|TOTP|JSON|CSV|PDF|NTLM|LM|hashcat|KrakenHashes|GPU|CPU|H\/s|[A-Z]{2,6}|\{\{[\w.]+\}\})$/;

/** True when a translation is just the English text copied over. */
export const isUntranslatedCopy = (en, tr) =>
  typeof en === 'string' && tr === en && en.length > 3 && !SAME_OK.test(en.trim());

// ---- string checks (also embedded in the workflow script) -----------------

export const placeholders = (s) => [...String(s).matchAll(/\{\{\s*([\w.]+)\s*(?:,[^}]*)?\}\}/g)].map((m) => m[1]).sort();
export const tags = (s) => [...String(s).matchAll(/<\/?([A-Za-z0-9]+)\s*\/?>/g)].map((m) => m[0].replace(/\s+/g, '')).sort();
export const sameList = (a, b) => a.length === b.length && a.every((x, i) => x === b[i]);

/** Problems with one translated string compared to its English source. */
export const checkString = (en, tr) => {
  const errors = [];
  if (typeof tr !== 'string' || tr.trim() === '') errors.push('empty');
  else {
    if (!sameList(placeholders(en), placeholders(tr))) errors.push(`placeholders differ: en ${JSON.stringify(placeholders(en))} vs ${JSON.stringify(placeholders(tr))}`);
    if (!sameList(tags(en), tags(tr))) errors.push(`markup tags differ: en ${JSON.stringify(tags(en))} vs ${JSON.stringify(tags(tr))}`);
  }
  return errors;
};
