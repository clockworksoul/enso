# OpenClaw host adapters

OpenClaw-specific driving adapters for Ensō live under this directory. Host
protocols, session identifiers, plugin configuration, and telemetry stay here;
none enter the canonical Markdown corpus or `internal/core`.

The initial adapter is [`enso-memory`](enso-memory/README.md), an
observation-only OpenClaw plugin that invokes the existing read-only
`cmd/enso-recall` bridge alongside the host's current memory-slot owner.
