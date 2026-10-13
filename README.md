# Ensō

> 円相 — the Zen circle drawn in one uninterrupted brushstroke, deliberately left open. The gap where the brush lifts is not a flaw; it is the form.

Ensō is an AI memory system designed to help an assistant avoid treating an
old belief as current. It keeps memories in readable Markdown files and ranks
them using both their connection to a question and how current or useful they
have been.

## What makes Ensō different

- **You can inspect the memory.** The Markdown files are the source of truth,
  so a person can read, edit, back up, or move them without Ensō. The search
  index is only a rebuildable helper.
- **When a fact changes, its history stays.** Ensō adds the new version and
  marks the old one as superseded instead of deleting or silently replacing
  it. That makes it possible to see what was believed before and when it
  changed.
- **Recall weighs fit and freshness together.** A close match to the question
  matters, but a very weak match can lose to a much fresher memory. Ensō can
  also follow links between related memories.
- **OpenClaw can learn from use.** Its observer looks for distinctive wording
  from a recalled memory in the assistant's reply. When it finds a match, it
  appends a reinforcement record; it does not rewrite the existing memory.
  This is a cautious text-matching signal, not a complete measure of meaning.

Try the command-line tools on your own memory folder, or connect Ensō to an
assistant.

## One memory across AI assistants

Ensō's memory format and recall engine are shared across assistants. A
small adapter connects each assistant platform (sometimes called a
"harness") to the same Markdown memories and `enso-recall` command. Changing
assistants does not require changing or exporting the memory store; each
platform just needs its own adapter to pass questions in and context back.

```text
OpenClaw ─┐
Codex ────┼──> Ensō recall ──> your Markdown memories
Hermes ───┘    (shared)       (portable and readable)
```

OpenClaw and Codex adapters are available today. **The near-term plan is to add
Hermes support next, then prioritize other widely used harnesses.** New
integrations will start in observation or shadow mode where possible, with
live context injection opt-in until the integration has been validated. Hermes
support is planned, not available yet.

## Try Ensō with your own notes

You need Go 1.26 or newer. From this repository, build the two command-line
tools:

```sh
go build -o ./enso-append ./cmd/enso-append
go build -o ./enso-recall ./cmd/enso-recall
```

Choose a folder for your Ensō memory, then add a fact. Replace the example text
with something you want your assistant to remember:

```sh
./enso-append \
  -root "$HOME/my-enso-memory" \
  -type Fact \
  -content "The community garden meeting is on Thursday at 6 pm"
```

Ask Ensō to find it:

```sh
./enso-recall \
  -root "$HOME/my-enso-memory" \
  -query "When is the garden meeting?"
```

The first command creates a `memory/` folder and adds a dated Markdown file.
The second prints ranked results as JSON. The default ranking weighs both how
closely a memory matches your question and how current it is. For recent
memories without a question, leave out `-query`:

```sh
./enso-recall -root "$HOME/my-enso-memory"
```

For example, a successful search returns JSON like this:

```json
{
  "mode": "lexical",
  "results": [
    {
      "id": "mem:2026-10-13-garden-meeting",
      "type": "Fact",
      "content": "The community garden meeting is on Thursday at 6 pm",
      "score": 0.82
    }
  ]
}
```

Results are listed best match first. The score helps order results; it is not a
probability that the memory is true. Your IDs and scores will differ.

The first recall may build a search index under `.enso/` in your memory folder.
It can be deleted and rebuilt; your Markdown memories remain the source of
truth. By default, recall works without an API key. If you set
`GEMINI_API_KEY`, Ensō also uses Google's Gemini embedding API to find related
memories that use different words. This sends the query and memory text used
for indexing to that service; leave the key unset to keep recall lexical-only.

### Adding or correcting a memory

Use `enso-append` to add a `Fact`, `Decision`, `Insight`, `Person`, `Project`,
or `Task`. For example:

```sh
./enso-append \
  -root "$HOME/my-enso-memory" \
  -type Decision \
  -content "Use the north entrance while the main gate is repaired"
```

When a fact changes, add the new version and point to the old entry with
`-supersede mem:YYYY-MM-DD-old-entry-id`. Ensō keeps the old version in the
Markdown history and marks it as superseded; it does not erase it. See
[`FORMAT.md`](FORMAT.md) for the memory types and file format.

## Connect Ensō to an assistant

The integrations are optional. They use the same Markdown memory folder and
`enso-recall` command.

- **OpenClaw:** The plugin observes recall alongside your existing memory
  plugin by default. Set `liveRecall: true` to also prepend a small number of
  high-scoring memories as supplementary historical context. It does not take
  over OpenClaw's `memory` slot. When its reply-use check finds that a recalled
  memory was materially used, it appends a reinforcement record to the memory
  even when `liveRecall` is off; this preserves the history rather than
  editing it in place. Start with the
  [OpenClaw setup guide](host/openclaw/enso-memory/README.md).
- **Codex:** The plugin defaults to `shadow` mode, which records recall
  metadata without adding context. Explicitly select `live` mode to inject
  bounded, labeled historical context. A conservative reply-use check appends
  reinforcement records when a reply materially uses a recalled memory, in
  either mode. See the
  [Codex setup guide](host/codex/enso-memory/README.md).

Both integrations are designed to fail safely: a recall or logging problem
should not stop your assistant from answering. OpenClaw's reinforcement is an
append-only corpus write; the assistant integrations do not silently edit or
delete existing Markdown records.

## What to expect

Ensō is an active project. OpenClaw slot ownership has not been decided; its
live option adds context while leaving the existing memory plugin in place.
The memory-worthiness classifier in this repository is an experimental tool,
not an automatic filter on what gets saved. For the latest project status, see
[`STATUS-LOG.md`](STATUS-LOG.md).

## Contributing

For development setup, tests, architecture, project invariants, benchmark
limitations, and documentation-maintenance guidance, see
[`CONTRIBUTING.md`](CONTRIBUTING.md).

## License

Apache License 2.0 — see [`LICENSE`](LICENSE) and [`NOTICE`](NOTICE).
