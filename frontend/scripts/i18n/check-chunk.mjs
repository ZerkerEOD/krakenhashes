#!/usr/bin/env node
// Usage: node scripts/i18n/check-chunk.mjs <chunk.json> <result.json>
// Verifies a translated chunk before merge: valid JSON, every id translated,
// no unknown ids, placeholders and markup tags identical to the English.
// Prints one line per problem; exit 1 when there are problems.
import { checkString, readJson } from './lib.mjs';

const [chunkFile, resultFile] = process.argv.slice(2);
if (!chunkFile || !resultFile) {
  console.error('usage: check-chunk.mjs <chunk.json> <result.json>');
  process.exit(2);
}
const chunk = readJson(chunkFile);
let result;
try {
  result = readJson(resultFile);
} catch (e) {
  console.log(`invalid JSON in ${resultFile}: ${e.message}`);
  process.exit(1);
}
const tr = result.translations ?? result;
const problems = [];
const ids = new Set();
for (const it of chunk.items) {
  ids.add(it.id);
  if (!(it.id in tr)) {
    problems.push(`${it.id}: missing`);
    continue;
  }
  for (const e of checkString(it.en, tr[it.id])) problems.push(`${it.id}: ${e}`);
}
for (const id of Object.keys(tr)) if (!ids.has(id)) problems.push(`${id}: not in this chunk`);
for (const p of problems) console.log(p);
console.log(problems.length ? `${problems.length} problems` : `ok: ${chunk.items.length} strings`);
process.exit(problems.length ? 1 : 0);
