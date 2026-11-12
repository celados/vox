# vox

An agent-native CLI built with [argc](https://github.com/ethan-huo/argc) on Bun.
Repo: https://github.com/celados/vox

- **Building this tool** — use the `argc` skill. It owns the schema design,
  handler, stdout, and release conventions; don't restate them here.
- **Using this tool** — `src/index.md` is the source of truth, served by
  `vox @skill`. `skills/vox/SKILL.md` is the harness stub (intent matching
  and immediate `@skill` routing; its body is only a fallback).
- **Releasing this tool** — use `.agents/skills/release/SKILL.md`; release is a
  `package.json` version bump pushed to `main`, then the workflow tags and
  publishes.
- **Runtime is Bun** — prefer its native APIs and check the source of truth at
  <https://bun.sh/llms.txt> instead of guessing from memory.

## Local binary

Global `vox` on this machine is **`bun link`** → `package.json` `bin` →
**`./src/main.ts`**.

- Do **not** install or commit a `dist/` binary for day-to-day use.
- After pulling, `bun install && bun link` if PATH is stale; then `vox --version`
  should match `package.json`.
- `dist/` is gitignored. Only CI builds it for GitHub Release assets.

## Storage

On-disk state stays at `~/.vox/` (state, runs, vocabularies, TTS cache). The
TypeScript rewrite reads the same run layout as before: `runs/<sid>/meta.yaml`,
`run.json`, `audio.<ext>`. Run ids are still
`sha256(audio || Go-shaped args JSON)` so existing stored runs remain cache hits.

Playback uses `afplay` (macOS) or `ffplay`/`aplay`. Microphone enrollment uses
`ffmpeg`. `ffprobe` is optional for non-WAV duration checks.

## Testing

`bun test` is offline: store, vocab, export, schema, keyless CLI paths. Provider
keys are `secret()` `apiKey` fields from `src/auth.ts` (`@celados/argc/ref`;
default chain env, then latch `celados-agent` via `@celados/argc-latch`); tests
pass env vars or `{ ref: 'env:…' }` and never call latch. Live DashScope calls are not mocked — run `vox hear` / `vox say` by hand
against a real key.

## Local install after every change

On the user's machines `vox` is the release bundle in `~/.local/bin`,
installed by the workspace `update-local-tool` skill. A change is not done when
it merges or releases. It is done when the user's local `vox` runs it. The
user does not watch for this, so do it without being asked:

1. After the release workflow for your change succeeds, check whether the tool is
   installed: `command -v vox`. If it is not, stop here.
2. Run `bash ~/workspace/.agents/skills/update-local-tool/update.sh vox`. It prints `before -> after`.
3. Check your change with the installed binary, for example `vox @schema` or the
   command you changed.
4. Put the `before -> after` line in your final report.

If the release failed, report that. Don't install a local build in its place.
