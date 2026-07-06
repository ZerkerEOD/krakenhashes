# Custom Charsets & Hashcat Arguments

KrakenHashes lets you fine-tune mask attacks with **custom character sets** and pass **additional
hashcat arguments** on a per-job basis. Both are optional and live in the Create Job dialog (and the
admin Preset Job form). This page covers what you can configure and the safety limits that apply.

## Custom character sets

A mask attack builds candidates from placeholders. Alongside hashcat's built-in sets
(`?l` lowercase, `?u` uppercase, `?d` digits, `?s` symbols, `?a` all, `?b` raw bytes) you can define
up to **four custom charsets** and reference them in your mask as `?1`, `?2`, `?3`, and `?4`.

For example, with charset **1** defined as `?u?l?d`, the mask `?1?1?1?1?1?1?1?1` brute-forces all
eight-character strings drawn from uppercase + lowercase + digits.

### Defining a charset

Each of the four slots (labeled **Charset 1 (-1)** through **Charset 4 (-4)**) can be filled two
ways:

- **Inline** — type the definition directly. It can mix built-in tokens and literal characters, for
  example `?u?d` (uppercase + digits) or `abcdef0123456789`.
- **Saved charset** — pick one from the **Saved** dropdown next to the slot. The list includes your
  personal charsets and any global charsets an administrator has published (see
  [Managing saved charsets](#managing-saved-charsets)). A saved charset can be a stored definition
  or an uploaded `.hcchr` file.

As you build the mask, a live **estimated keyspace** chip shows how large the attack will be, along
with the size of each referenced charset.

### Hex-encoded charsets

Toggle **Hex-encoded charsets** when your charset is expressed as hexadecimal byte pairs — for
example `41424344` represents the bytes `ABCD`. This is useful for non-printable or binary
characters. The toggle applies to **inline** definitions; file-based (`.hcchr`) charsets are
interpreted by hashcat directly and are unaffected. Selecting a saved charset that is marked **Hex**
turns the toggle on automatically. Under the hood this enables hashcat's `--hex-charset` mode — you
do not pass that flag yourself (see [blocked flags](#what-you-cannot-pass)).

### `.hcchr` charset files

For complex character sets you can upload a hashcat charset file (`.hcchr`) as a saved charset, then
select it in a slot. Only `.hcchr` files are accepted, and the file content is validated on upload.
KrakenHashes automatically synchronizes the file to the agents that run your job, so you don't need
to distribute it manually.

### Managing saved charsets

Saved charsets come in two scopes:

| Scope | Where to manage | Availability |
|-------|-----------------|--------------|
| **Personal** | **Settings → Charsets** | Your charsets, visible to you |
| **Global** | **Admin → Custom Charsets** (administrators) | Published to all users |

In either place you can create an inline charset (name + definition, optionally hex) or upload a
`.hcchr` file. Both scopes appear together in the **Saved** picker when you build a job's charsets.

## Additional hashcat arguments

The **Additional Hashcat Arguments (Optional)** field on a custom job (and on admin preset jobs as
`additional_args`) lets you append extra hashcat flags for that job — for example performance tuning:

```
-w 4 -O --force
```

(`-w 4` sets the workload profile, `-O` enables the optimized kernel, `--force` ignores warnings.)

!!! info "Agent flags take precedence"
    If an agent defines its own extra hashcat parameters, those take priority over a job's
    additional arguments when the two conflict.

### Limits

To keep jobs safe and predictable, the field is validated before the job is created:

- **Length:** at most **500 characters**.
- **No shell metacharacters:** the characters `;` `|` `&` `` ` `` `$` `(` `)` `{` `}` `<` `>` `\`
  (and line breaks — both newline and carriage return) are rejected.
- **Blocked flags:** a set of hashcat flags is rejected because KrakenHashes controls them or they
  would be unsafe (see below). If you use one, the job is rejected with a message explaining which
  flag and why.

### What you cannot pass

Blocked flags fall into these categories:

| Category | Examples | Why |
|----------|----------|-----|
| Set by the job | `-m`/`--hash-type`, `-a`/`--attack-mode`, `-r`/`--rules-file`, `-1`…`-8` custom charsets, `-i`/`--increment` | These come from the job configuration (including the charsets and hex mode above). |
| Set by the chunking system | `-s`/`--skip`, `-l`/`--limit`, `--keyspace` | The scheduler divides work into chunks. |
| Managed by the agent | `-d`/`--backend-devices`, `-o`/`--outfile`, `--session`, `--potfile-*`, `--restore*`, `--status*` | Devices, output, sessions, and potfiles are agent-controlled. |
| File-path injection risk | `--debug-file`, `--induction-dir`, `--markov-hcstat2`, `--*-keyfiles` | Arbitrary file paths are disallowed for security. |
| Would stop cracking | `-b`/`--benchmark`, `--show`, `--left`, `--stdout`, `-V`/`--version`, `-h`/`--help`, `--identify` | These switch hashcat into a non-cracking mode. |
| Hex charset | `--hex-charset` | Managed by the **Hex-encoded charsets** toggle instead. |

Stick to performance and tuning flags (workload, optimized kernel, etc.). Flags that control the
attack itself, I/O, devices, output, or files are on the blocklist and rejected. The check is a
blocklist on the flag name — a flag that isn't on it is passed through to hashcat as-is, so
double-check any unfamiliar flag before relying on it.

## See also

- [Jobs & Workflows](jobs-workflows.md) — creating and configuring jobs
- [Wordlist Filtering](wordlist-filtering.md) — another way to tune the candidate space
- [Hash Types](../reference/hash-types.md) — supported algorithms
