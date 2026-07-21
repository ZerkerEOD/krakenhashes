# Loopback: Re-run Against New Cracks

## Quick Overview

**Loopback** takes the passwords an attack just cracked and feeds them straight back into
the *mutating* part of that same attack — automatically, over and over, until a pass finds
nothing new.

The idea comes from a simple observation: people reuse patterns. If `Summer2024` cracked,
then `Summer2024!`, `Summer2024#`, or `summer2024` are good guesses for the *next* hash. A
loopback applies your rules (or a hybrid mask) to the freshly-cracked plaintexts only — a
tiny, high-value candidate set — instead of re-running the whole wordlist.

!!! tip "Why not just add the attack to a workflow twice?"
    Running the same attack again re-tries the *entire* wordlist for a handful of new
    candidates. Loopback re-runs the mutation against **only** the newly-cracked passwords
    (the "delta"), so each round is small and fast, and the loop stops on its own once it
    goes dry.

## How It Works

1. Your attack runs normally and cracks some hashes.
2. When it finishes, KrakenHashes collects the **delta** — the distinct new plaintexts that
   *this run* cracked.
3. It re-runs the attack's **mutation** against just those plaintexts:
    - **Straight + rules** → the delta words × your rules
    - **Hybrid (wordlist + mask / mask + wordlist)** → the delta words ± your mask
4. Any hashes cracked in that round become the *next* delta, and the process repeats.
5. The session stops when a round produces **no new plaintext** ("dry"), when the whole
   hashlist is cracked, or when the safety cap on rounds is reached.

Each distinct plaintext is used as loopback input **exactly once**, so the loop is
guaranteed to terminate — it can never re-try the same candidate twice.

### What's eligible

Loopback only makes sense for attacks that have a *mutation* separable from the wordlist:

| Attack | Eligible? | Behavior |
| --- | --- | --- |
| Straight (dictionary) **with rules** | ✅ Yes | Delta words re-run through the rules |
| Hybrid wordlist + mask (`-a 6`) | ✅ Yes | Delta words re-run with the mask appended |
| Hybrid mask + wordlist (`-a 7`) | ✅ Yes | Delta words re-run with the mask prepended |
| Straight **without rules** | ❌ No | The word was already tried as-is — nothing to mutate |
| Brute-force / mask (`-a 3`) | ❌ No | No wordlist to feed the delta into |
| Combination (`-a 1`) | ❌ No | No single "mutation" side |
| Association (`-a 9`) | ❌ No | 1:1 hash↔word mapping — no loopback semantics |

!!! note "Ineligible steps still contribute"
    In a workflow, an ineligible step (say, a brute-force step) isn't re-run, but the new
    passwords **it** cracks are still added to the delta pool that the eligible steps loop
    on. Nothing a step cracks is wasted.

## Turning Loopback On

There are three places to enable loopback, depending on how you launch the attack.

### 1. In a Workflow (admin)

When building or editing a **Job Workflow** (Admin → Preset Jobs & Workflows):

- **Enable loopback for all eligible steps** — a master toggle at the top of the workflow
  form. When on, every eligible step in the workflow loops back, and the per-step checkboxes
  are ignored.
- **Per-step "Loopback"** — a checkbox on each step, used only while the master toggle is
  **off**. It's disabled (with an explanatory tooltip) for ineligible steps.

A workflow that has loopback configured shows a **"Loopback: all eligible"** badge in the
create-job dialog so operators know it will loop automatically — there's nothing extra to
turn on at run time.

### 2. On a Preset Job run

In the **Create Job** dialog's **Preset** tab, a **"Loopback until dry"** toggle appears
above the preset list. It enables once you've selected an eligible preset (straight + rules,
or a hybrid attack). The selected job runs, then its mutation loops against the new cracks.

### 3. On a Custom job run

In the **Create Job** dialog's **Custom** tab, a **"Loopback until dry"** toggle appears for
eligible attacks (rules or a hybrid mask).

!!! warning "Loopback and pre-filtering are mutually exclusive"
    On a custom job you can use **either** wordlist [pre-filtering](wordlist-filtering.md)
    **or** loopback, not both at once. The loopback toggle is unavailable while pre-filtering
    is enabled for that job.

## Watching a Loopback Run

While any loopback is in flight, a **Loopback** panel appears on both the **Jobs** page and
the **Dashboard**. It hides itself when there's nothing running.

Each session shows:

- Its **name** and a **source** chip — `workflow`, `preset`, or `custom`.
- A **status** chip:

    | Status | Meaning |
    | --- | --- |
    | **Waiting for round to finish** | The current round's jobs are still running |
    | **Looping** | A delta round has been spawned and is being monitored |
    | **Done** | A round came back dry (or the round cap was hit) — the loop finished |
    | **Failed** | The session hit an error (see the tooltip) |
    | **Cancelled** | The session was cancelled |

- **Round X / Y** — the current round and the configured safety cap.
- The number of jobs in the session, expandable to see each round's jobs and their status.

Loopback re-runs are ordinary jobs, so they also appear in the normal Jobs list with their
own progress bars, priority, and agent assignment — the panel just ties them together.

!!! note "Sessions survive a restart"
    The loopback controller is durable: if the backend restarts while a session is waiting on
    a long round, the session resumes rather than being lost.

## The Round Cap

Every session has a safety cap on how many delta rounds it will run, even if new cracks keep
appearing. The default is **10 rounds**. Administrators can change it — see
[Job Settings](../admin-guide/operations/job-settings.md#loopback-round-cap). In practice
most sessions go dry well before the cap.

## Related

- [Jobs and Workflows](jobs-workflows.md)
- [Wordlist Filtering](wordlist-filtering.md)
- [Loopback architecture](../reference/architecture/loopback.md) (how it works internally)
