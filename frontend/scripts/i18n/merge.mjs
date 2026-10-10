#!/usr/bin/env node
// Usage: node scripts/i18n/merge.mjs <lang> <result.json>... [--force]
// A result file is {"translations": {"<ns>:<key>": "<text>", ...}} (or a bare map).
// Writes into public/locales/<lang>/<ns>.json in English key order. Existing
// non-empty translations are kept unless --force. Keys absent from English are
// kept (appended) and reported, never silently dropped.
import { SOURCE, expectedKeys, flatten, loadNs, namespaces, readJson, writeNs } from './lib.mjs';

const args = process.argv.slice(2);
const force = args.includes('--force');
const [lang, ...files] = args.filter((a) => a !== '--force');
if (!lang || files.length === 0) {
  console.error('usage: merge.mjs <lang> <result.json>... [--force]');
  process.exit(2);
}
if (lang === SOURCE) {
  console.error('refusing to merge into the source language');
  process.exit(2);
}

const incoming = new Map();
for (const f of files) {
  const data = readJson(f);
  for (const [id, text] of Object.entries(data.translations ?? data)) incoming.set(id, text);
}

let written = 0;
let kept = 0;
const unknown = [];
for (const ns of namespaces()) {
  const current = new Map(flatten(loadNs(lang, ns)));
  const expected = expectedKeys(lang, ns);
  const expectedSet = new Set(expected.map((r) => r.key));
  const out = [];
  for (const { key } of expected) {
    const id = `${ns}:${key}`;
    const cur = current.get(key);
    const hasCur = typeof cur === 'string' && cur.trim() !== '';
    if (incoming.has(id) && (force || !hasCur)) {
      out.push([key, incoming.get(id)]);
      written++;
      incoming.delete(id);
    } else if (hasCur) {
      out.push([key, cur]);
      if (incoming.has(id)) {
        kept++;
        incoming.delete(id);
      }
    }
  }
  for (const [key, value] of current) if (!expectedSet.has(key)) out.push([key, value]);
  if (out.length) writeNs(lang, ns, out);
}
for (const id of incoming.keys()) unknown.push(id);
console.log(`${lang}: wrote ${written}, kept existing ${kept}${force ? '' : ' (use --force to overwrite)'}`);
if (unknown.length) {
  console.log(`ignored ${unknown.length} ids not in English, e.g. ${unknown.slice(0, 5).join(', ')}`);
}
