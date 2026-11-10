# Security

This repository is an unsupported research experiment by [IsoKron](https://isokron.ai),
based on `github/github-mcp-server` v2.1.0. It is not an official GitHub release, it
receives no security updates, and it is not meant for production use.

**Do not run it with a token that can reach anything you care about.** If you run it at
all, use a dedicated test organization and a fine-grained, short-lived token limited to
disposable repositories.

## Reporting

- **Vulnerabilities in the official GitHub MCP server** (anything that also exists in
  upstream): report them to GitHub through the official project's
  [security policy](https://github.com/github/github-mcp-server/security/policy), not here.
- **Problems specific to the experimental changes** in this repository (see
  [CHANGES-EXPERIMENT.md](CHANGES-EXPERIMENT.md)): email hello@isokron.ai, or open an
  issue here if the problem is not sensitive.

Reports are read, but no response time or fix is promised.
