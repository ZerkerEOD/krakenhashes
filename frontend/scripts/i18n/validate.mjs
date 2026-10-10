#!/usr/bin/env node
// Usage: node scripts/i18n/validate.mjs [lang...] [--allow-missing] [--json]
// Errors (exit 1): invalid JSON, missing keys (unless --allow-missing), empty
// strings, placeholder or markup-tag mismatch, missing plural categories.
// Warnings: text identical to English, > 1.8x longer than English, extra keys.
import fs from 'node:fs';
import { SOURCE, checkString, expectedKeys, flatten, isUntranslatedCopy, languages, namespaces, nsPath, readJson } from './lib.mjs';

const args = process.argv.slice(2);
const allowMissing = args.includes('--allow-missing');
const asJson = args.includes('--json');
const langs = args.filter((a) => !a.startsWith('--'));
const targets = (langs.length ? langs : languages()).filter((l) => l !== SOURCE);


let totalErrors = 0;
const report = {};
for (const ns of namespaces()) {
  try {
    readJson(nsPath(SOURCE, ns));
  } catch (e) {
    console.error(`en/${ns}.json: invalid JSON: ${e.message}`);
    process.exit(1);
  }
}
for (const lang of targets) {
  const r = { errors: [], warnings: [], missing: 0, total: 0 };
  for (const ns of namespaces()) {
    const file = nsPath(lang, ns);
    let have = new Map();
    if (fs.existsSync(file)) {
      try {
        have = new Map(flatten(readJson(file)));
      } catch (e) {
        r.errors.push(`${ns}.json: invalid JSON: ${e.message}`);
        continue;
      }
    }
    const expected = expectedKeys(lang, ns);
    const expectedSet = new Set(expected.map((x) => x.key));
    for (const row of expected) {
      r.total++;
      const tr = have.get(row.key);
      if (tr === undefined) {
        r.missing++;
        if (!allowMissing) r.errors.push(`${ns}:${row.key}: missing`);
        continue;
      }
      for (const e of checkString(row.en, tr)) r.errors.push(`${ns}:${row.key}: ${e}`);
      if (typeof tr === 'string' && typeof row.en === 'string') {
        if (isUntranslatedCopy(row.en, tr)) r.warnings.push(`${ns}:${row.key}: identical to English`);
        if (row.en.length > 8 && tr.length > row.en.length * 1.8) r.warnings.push(`${ns}:${row.key}: ${tr.length} chars vs ${row.en.length} in English`);
      }
    }
    for (const key of have.keys()) if (!expectedSet.has(key)) r.warnings.push(`${ns}:${key}: not in English (stale?)`);
  }
  totalErrors += r.errors.length;
  report[lang] = r;
}

if (asJson) {
  console.log(JSON.stringify(report, null, 2));
} else {
  for (const [lang, r] of Object.entries(report)) {
    const pct = r.total ? (((r.total - r.missing) / r.total) * 100).toFixed(1) : '0';
    console.log(`${lang}: ${pct}% (${r.total - r.missing}/${r.total}), ${r.errors.length} errors, ${r.warnings.length} warnings`);
    for (const e of r.errors.slice(0, 25)) console.log(`  error   ${e}`);
    if (r.errors.length > 25) console.log(`  … ${r.errors.length - 25} more errors`);
  }
}
process.exitCode = totalErrors ? 1 : 0; // not process.exit(): it can truncate piped output
