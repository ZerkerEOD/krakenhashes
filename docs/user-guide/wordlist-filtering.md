# Wordlist Filtering

Wordlist filtering lets you derive a smaller, targeted wordlist from a larger one by keeping only
the words that match criteria you choose — for example, "only words 8–16 characters long that
contain an uppercase letter, a digit, and a symbol." This trims candidates that can't possibly
satisfy a known password policy, so jobs finish faster without wasting work.

You can filter in two ways:

- **For a single job (on the fly):** apply a filter while creating a custom job. KrakenHashes builds
  a temporary, job-scoped wordlist that exists only for that job.
- **As a reusable wordlist:** create a permanent **filtered wordlist** on the Wordlists page that you
  can select in any future job, just like any other wordlist.

## Filter criteria

The same criteria are available in both places. A word is kept **only if it satisfies every
criterion you set** (the criteria are combined with AND). Leave a field blank to ignore it.

| Criterion | Meaning |
|-----------|---------|
| **Min length** / **Max length** | Bounds on word length, measured in characters (not bytes — multi-byte characters count as one). |
| **Required character classes** | Require any of: **Uppercase**, **Lowercase**, **Digit**, **Special** (a printable ASCII symbol — non-alphanumeric). Each box you check must be present in the word. |
| **Min # of classes (1–4)** | Require at least *N* of the four classes to be present — e.g. `3` keeps words that mix at least three of upper/lower/digit/special. |
| **Regex (RE2)** | A regular expression the word must match. Uses the Go **RE2** engine, which runs in linear time (no catastrophic backtracking). |

!!! warning "Regex engine: RE2, no lookarounds"
    The regex field uses RE2, which does **not** support lookaround assertions
    (`(?=…)`, `(?!…)`, `(?<=…)`, `(?<!…)`). The form flags these as you type. Most "require a
    class" rules are better expressed with the length and character-class fields above than with a
    regex. A simple length pattern like `^.{10,16}$` is fine.

If two criteria overlap, the **stricter one wins** — e.g. setting min length 8 but a regex that
requires 12+ characters effectively requires 12+.

## Live preview

When a source wordlist is selected, the form shows a live estimate such as
`~1,200,000 candidates (24.3% of sample)`. This is produced by **sampling the start of the source
wordlist** (the first ~1,000,000 lines) and extrapolating, so it is fast even for huge files.

!!! note "Estimates are sampled"
    Because the preview samples the beginning of the file, the true total can differ for wordlists
    that are sorted or front-loaded (for example, a list ordered shortest-to-longest will skew a
    length filter's estimate). Treat the number as a guide, not an exact count.

## Filtering a wordlist for a single job

When you create a **custom job**, enable the wordlist filter option, choose your source
wordlist(s), and set the criteria. On submit:

1. The job is created in a **`preparing`** state while KrakenHashes generates the temporary
   (job-scoped) filtered wordlist in the background.
2. Once generation finishes, the job automatically moves to **`pending`** and the scheduler picks
   it up — no further action needed.
3. If generation fails (for example, the filter matches zero words), the job is marked **failed**
   with the reason, and the temporary wordlist is cleaned up.

These job-scoped filtered wordlists are temporary: they belong to the job that created them and are
not added to your general wordlist library.

## Creating a reusable (permanent) filtered wordlist

If you'll reuse a filter, create a permanent filtered wordlist instead:

1. Go to the **Wordlists** page and click **Filtered Wordlist**.
2. Pick a **Source wordlist**. Only verified, non-potfile, non-filtered wordlists can be used as a
   source.
3. Give it a **Name** (a default like `MyList (filtered)` is suggested).
4. Set your **filter criteria** and check the live preview.
5. Click **Create**.

The wordlist is generated in the background. It appears in the wordlist table immediately with a
**Filtered** badge and a status that progresses through:

| Status | Meaning |
|--------|---------|
| **Pending** | Generation in progress (first build). |
| **Verified** | Ready to use — selectable in jobs and downloadable. |
| **Failed** | Generation failed (e.g. the filter matched no words). |
| **Regenerating…** | The parent changed and the filtered list is being rebuilt. |
| **Stale** | The parent changed since this was generated; a refresh is queued or can be triggered manually. |

Once **Verified**, a filtered wordlist behaves exactly like any other wordlist — select it when
creating jobs, download it, edit its metadata, or delete it.

## Keeping filtered wordlists up to date

A filtered wordlist remembers the parent it was derived from.

- **Automatic regeneration:** When the parent wordlist changes, KrakenHashes regenerates the
  filtered copy **automatically**. When the parent was only **appended to** (new words added to the
  end), it does this **incrementally** — only the new lines are filtered and appended — which is
  fast even for large lists.
- **Manual full regeneration:** The **Regenerate** (↻) action forces a complete rebuild from the
  parent. You normally don't need this; use it only if the parent's existing words were **edited,
  removed, or reordered** (not just appended), since the incremental path assumes append-only
  changes. A full regeneration re-filters the entire parent and can take a while for large lists.

## See also

- [Wordlists & Rules](wordlists-rules.md) — working with wordlists and rules in general
- [Jobs & Workflows](jobs-workflows.md) — creating jobs (where on-the-fly filtering lives)
- [Wordlist Management (admin)](../admin-guide/resource-management/wordlists.md) — managing the
  wordlist library
