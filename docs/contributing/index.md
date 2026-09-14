# Contributing to KrakenHashes

Thank you for your interest in contributing to KrakenHashes! This section contains guides for different ways you can help improve the project.

## Ways to Contribute

<div class="grid cards" markdown>

-   :material-translate:{ .lg .middle } **[Translations](translations.md)**

    ---

    Help make KrakenHashes accessible to users worldwide by contributing translations to your language.

-   :material-bug:{ .lg .middle } **Bug Reports**

    ---

    Found a bug? [Open an issue](https://github.com/ZerkerEOD/krakenhashes/issues/new) on GitHub with details to reproduce it.

-   :material-lightbulb:{ .lg .middle } **Feature Requests**

    ---

    Have an idea? [Start a discussion](https://github.com/ZerkerEOD/krakenhashes/discussions) or open an issue to propose new features.

-   :material-source-pull:{ .lg .middle } **Code Contributions**

    ---

    Submit pull requests for bug fixes, features, or documentation improvements.

</div>

## Getting Started

1. **Fork the repository** on [GitHub](https://github.com/ZerkerEOD/krakenhashes)
2. **Clone your fork** locally
3. **Create a branch** for your contribution
4. **Make your changes** following the relevant guide
5. **Submit a pull request** with a clear description

## Editing the documentation

Build the site locally before pushing, so a broken link is caught by you rather than by CI:

```bash
scripts/docs-build.sh          # strict build -- the pre-push check
scripts/docs-build.sh serve    # live-reloading preview on http://127.0.0.1:8000
scripts/docs-build.sh clean    # remove the virtualenv and the built site
```

The first run creates a Python virtualenv and installs `requirements-docs.txt` into it,
which takes a minute; after that a build is about ten seconds. Dependencies are only
reinstalled when `requirements-docs.txt` changes.

!!! note "Why a script rather than plain `pip install`"
    Arch, Debian 12+, Fedora and Ubuntu 23.10+ ship [PEP 668](https://peps.python.org/pep-0668/)
    "externally managed" Pythons, which refuse `pip install` outside a virtualenv. CI installs
    onto a clean runner and does not hit this; the script owns a virtualenv so your machine
    matches CI without you managing one. It lives outside the repository, so it is never
    committed and survives `git clean`.

`--strict` fails the build on mkdocs **warnings**, which includes a link to a file that does
not exist. Unresolved *anchors* are reported at `INFO` and do **not** fail the build, so read
the output as well as the exit code when adding cross-references.

New pages are picked up automatically. Adding one to `mkdocs.yml`'s `nav` puts it in the
sidebar; leaving it out means it is reachable only from links on other pages, which is the
existing convention for provider- and mode-specific sub-pages.

## Community

Join our [Discord server](https://discord.gg/taafA9cSFV) to connect with other contributors and get help with your contributions.

## Code of Conduct

Please be respectful and constructive in all interactions. We're building this together!
