# Security policy

## Supported versions

Security fixes target the [latest release](https://github.com/Giammarco-Ferranti/deja/releases/latest).
If you use an older version, upgrade before checking whether a finding still
occurs. Older releases do not receive security backports.

## Reporting a vulnerability

[Report a vulnerability privately through GitHub](https://github.com/Giammarco-Ferranti/deja/security/advisories/new).
You can also open the repository's Security tab and select "Report a vulnerability".
Keep vulnerability details out of public issues and pull requests.

Include the information needed to reproduce and assess the finding:

- The affected deja version, operating system, and zsh version.
- Reproduction steps or a small proof of concept using sample commands.
- The impact, including who could access data or trigger the problem.
- A proposed fix, if you have one.

Use sample values in place of credentials and personal command history.
Maintainers will discuss the report and any fix through the private advisory.

For ordinary bugs and feature requests, use the repository's
[issue templates](https://github.com/Giammarco-Ferranti/deja/issues/new/choose).

## Command history

deja stores command history in plaintext in a local SQLite database. It sets
the data directory to `0700` and database files to `0600`; these permissions
do not prevent access by processes running as your account or as root.
deja does not encrypt history or automatically redact secrets in commands.

Read the [privacy guide](docs/privacy.md) for history exclusions, data locations,
and removing existing data.
