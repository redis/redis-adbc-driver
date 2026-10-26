# Explicit verified TLS trust for partition workers

Date: 2026-10-21. Source: 20de269, merged #397. Author: Codex; requestor:
Albert Wong (continue partition compatibility work). Status: implemented;
validation in progress; human approval pending.
Issue: [#182](https://github.com/redis/redis-adbc-driver/issues/182).
See [spec](spec.md) and [plan](plan.md).

Linux private-CA validation uses SSL_CERT_FILE, which Go's macOS native verifier
does not honor the same way. Add explicit per-database PEM roots and required
macOS installed-wheel TLS validation without changing a host keychain. The user
asked about Let's Encrypt; no owned domain, DNS access or existing certificate
is available. No public certificate was requested, paid resource provisioned,
or system trust changed. Publicly trusted rediss endpoints keep their defaults.
