# Work order: adversarial review of the vox argc rewrite

Read this file and execute it. Do not modify source, tests, CI, skills, or
config. The only file you may create or overwrite is the artifact named below.

## Outcome

Independent review of the working-tree rewrite of `projects/vox` from a Go
CLI to an argc v7 / Bun TypeScript CLI. Judgment is the deliverable.

## Checkout

Working directory: `/Users/dio/workspace/projects/vox`

- Last committed Go implementation: `cea399279137c5b9d05d722985993d337357014d`
  (`git show cea3992:<path>`). The Go tree is deleted in the working tree;
  it still exists in git.
- Working-tree TypeScript implementation: `src/`, `package.json`,
  `.github/workflows/`, `src/index.md`, `AGENTS.md`.
- Last committed spec: `git show cea3992:docs/cli-schema.md`
- Working-tree spec: `docs/cli-schema.md`

## Documents to read

1. `git show cea3992:docs/cli-schema.md` — Go-era surface contract
2. `docs/cli-schema.md` — rewritten spec
3. `src/index.md` — agent usage guide served by `vox @skill`
4. `AGENTS.md` — local argc conventions
5. `src/schema.ts`, `src/handlers.ts`, `src/main.ts`
6. Domain modules: `src/run-store.ts`, `src/vocab.ts`, `src/export.ts`,
   `src/dashscope.ts`, `src/realtime.ts`, `src/hear.ts`, `src/tts.ts`,
   `src/audio.ts`, `src/config.ts`, `src/cli-error.ts`
7. Tests under `src/*.test.ts`
8. Enough of the Go sources via `git show cea3992:internal/...` and
   `git show cea3992:cmd/...` to check behavioral fidelity, especially
   run digest/sid, vocab resolve/hash/index, export segmentation,
   DashScope upload/transcription/TTS, and error codes.

Do not load or trust any prior review, chat summary, or author notes about
what was "intentional". Form your own view from the tree and git.

## Constraints

- argc v7: dotted commands, one structured input object, handler return
  value is stdout (strings are raw; objects are YAML), domain refusals use
  `domainError` / `DOMAIN_ERROR` with a matchable `code`.
- Existing `~/.vox/` data from the Go CLI must remain usable: same run
  digest, same sid length, same `runs/<sid>/{meta.yaml,run.json,audio.*}`
  and `vocabulary/` layout.
- Do not invent missing DashScope API behavior. Compare to the Go client.

## Non-goals

- Do not rewrite, restyle, or "fix" the code.
- Do not commit, tag, release, or `bun link`.
- Do not expand scope into TTS-on-runs or other backlog items.
- Do not write a second implementation.

## Review contract

Write falsifiable findings only. Each finding must cite `file:line` in the
working tree and/or a command whose output you captured. Classify:

- **must-fix** — correctness, security, data-model break vs Go `~/.vox/`,
  silent drop of a specified command or invariant
- **should-fix** — argc contract miss, error-envelope mismatch, test that
  cannot fail, agent-skill lie
- **nit** — naming, comments, extra machinery that does not change behavior

Questions to answer (do not skip):

1. Where the rewritten spec diverges from `cea3992` spec/Go, is the change
   a necessary argc adaptation or a silent behavior drop?
2. Is `sid = sha256(audio ‖ argsJSON)` byte-compatible with Go
   `encoding/json` (field order, omitempty, HTML escaping)? Prove or
   falsify with a concrete args example.
3. Do vocab content hashes and `.index.json` remain compatible?
4. Does `export` still emit the document itself on stdout when `output` is
   absent, and an index command never mix in the transcript?
5. Auth, hear, session, vocab, say, voice, cache: any command or flag from
   the Go CLI with no argc equivalent and no spec note?
6. Error codes: same closed set, or renamed/dropped so an existing agent
   skill would branch wrong?
7. Tests: what important behavior is untested? Which tests can only pass?

`bun test` and `git show` are allowed as evidence. A green suite is not
proof of correctness.

## Artifact

Write exactly:

`/Users/dio/workspace/projects/vox/docs/rewrite-argc.review.md`

OKF sidecar of `docs/cli-schema.md`:

```yaml
---
type: Review
title: Adversarial review of the argc TypeScript rewrite
resource: ./cli-schema.md
verdict: approve | request-changes
version: 0.1
generated: { by: claude/fable, at: <ISO-8601> }
---
```

Body structure:

1. Verdict in one paragraph
2. Findings table: severity, claim, evidence (file:line or command)
3. Spec deltas vs `cea3992` (justified vs dropped)
4. What you did not verify

No praise. No suggested rewrite of the rewrite unless a finding requires it.
If you have no must-fix findings, say so explicitly and still list should-fix
and unverified areas.

## Receipt

Final message only: status, artifact path, commands you ran, blockers.
Do not paste the review into the final message.
