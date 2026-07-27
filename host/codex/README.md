# Codex host adapter

Codex-specific driving adapters for Ensō live under this directory. Host
protocols, task/session identifiers, hook configuration, and telemetry stay
here; none enter the canonical Markdown corpus or `internal/core`.

The initial adapter is [`enso-memory`](enso-memory/README.md), a Codex plugin
that invokes the existing read-only `cmd/enso-recall` bridge from a
`UserPromptSubmit` hook.
