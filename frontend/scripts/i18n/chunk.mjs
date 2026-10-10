#!/usr/bin/env node
// Usage: node scripts/i18n/chunk.mjs <lang> [--out dir] [--max-items 250] [--max-words 1500] [--all] [--include-identical]
// Writes work chunks of untranslated keys for one language. --include-identical also
// re-sends strings that are a verbatim copy of the English; --all sends everything.
import fs from 'node:fs';
import path from 'node:path';
import { ROOT, SOURCE, expectedKeys, flatten, isUntranslatedCopy, loadContext, loadNs, namespaces, pluralCategories } from './lib.mjs';

const args = process.argv.slice(2);
const lang = args[0];
if (!lang || lang.startsWith('--')) {
  console.error('usage: chunk.mjs <lang> [--out dir] [--max-items N] [--max-words N] [--all]');
  process.exit(2);
}
const opt = (name, dflt) => {
  const i = args.indexOf(name);
  return i >= 0 ? args[i + 1] : dflt;
};
const outDir = opt('--out', path.join(ROOT, 'scripts', 'i18n', 'work', lang));
const maxItems = Number(opt('--max-items', 250));
const maxWords = Number(opt('--max-words', 1500));
const all = args.includes('--all');
const includeIdentical = args.includes('--include-identical');

const items = [];
for (const ns of namespaces()) {
  const have = new Map(flatten(loadNs(lang, ns)));
  const ctx = loadContext(ns);
  for (const row of expectedKeys(lang, ns)) {
    const cur = have.get(row.key);
    const translated = typeof cur === 'string' && cur.trim() !== '';
    const copy = translated && isUntranslatedCopy(row.en, cur);
    if (!all && translated && !(includeIdentical && copy)) continue;
    const item = { id: `${ns}:${row.key}`, ns, key: row.key, en: row.en };
    if (copy) item.current = cur;
    const note = ctx.get(row.base ?? row.key) ?? ctx.get(row.key);
    if (note) item.note = note;
    if (row.category) item.plural = row.category;
    items.push(item);
  }
}

fs.rmSync(outDir, { recursive: true, force: true });
fs.mkdirSync(outDir, { recursive: true });
const words = (s) => String(s ?? '').split(/\s+/).filter(Boolean).length;
let chunk = [];
let wc = 0;
let n = 0;
const flush = () => {
  if (!chunk.length) return;
  n++;
  const file = path.join(outDir, `chunk-${String(n).padStart(3, '0')}.json`);
  fs.writeFileSync(file, JSON.stringify({ lang, source: SOURCE, pluralCategories: pluralCategories(lang), items: chunk }, null, 2) + '\n');
  chunk = [];
  wc = 0;
};
let lastPrefix = '';
for (const it of items) {
  const prefix = `${it.ns}:${it.key.split('.')[0]}`;
  const w = words(it.en);
  // Prefer breaking at a top-level key boundary once the chunk is reasonably full.
  const full = chunk.length >= maxItems || wc + w > maxWords;
  const nearlyFull = chunk.length >= maxItems * 0.7 && prefix !== lastPrefix;
  if (full || nearlyFull) flush();
  chunk.push(it);
  wc += w;
  lastPrefix = prefix;
}
flush();
console.log(`${lang}: ${items.length} keys → ${n} chunks in ${path.relative(process.cwd(), outDir)}`);
