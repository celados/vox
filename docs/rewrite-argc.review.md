---
type: Review
title: Adversarial review of the argc TypeScript rewrite
resource: ./cli-schema.md
verdict: request-changes
version: 0.1
generated: { by: claude/fable, at: 2026-08-19T09:10:25Z }
---

# Adversarial review of the argc TypeScript rewrite

Reviewed: working tree at `main` (uncommitted rewrite) against
`cea399279137c5b9d05d722985993d337357014d` (last Go implementation and spec).
All Go references below are `git show cea3992:<path>`; all TS references are
working-tree `file:line`.

## 1. Verdict

Request changes, on one must-fix: `loadIndex` validates only JSON _syntax_,
not shape, so an `.index.json` whose content is valid JSON but not an object
(`[]`, a string, a number, a wrong-shaped object) loads as a usable index
instead of failing with `vocab_index_corrupt` — and `vocab.prune` run against
such an index computes zero claimed ids and deletes every hotword list on the
account. That is the exact failure mode the spec's own invariant exists to
prevent (`docs/cli-schema.md:190`), and Go fails closed on all four shapes
(empirically confirmed both ways, evidence under finding M1). Everything
structural otherwise holds: run digests, sids, and vocabulary content hashes
are byte-compatible with Go for realistic inputs (proven empirically against
Go 1.26.6, including the HTML-escape cases), the `~/.vox/` layout is
unchanged, the command surface is complete, and the error-code set is the
same closed twelve. The remaining should-fixes are error-envelope leaks, a
silently-dropped repeated-flag behavior with digest impact, an over-wide
recovery path in vocab sync, and two undocumented behavior changes in the
legacy TTS surface.

## 2. Findings

| #   | Severity   | Claim                                                                                                                                                                                                                                                                                                                                                                                                  | Evidence                                                                                                                                                                                                                                                                                                               |
| --- | ---------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| M1  | must-fix   | `loadIndex` accepts any syntactically valid JSON as an index; a shape-corrupt `.index.json` makes `vocab.prune` (without `--dryRun`) treat every remote list as an orphan and delete it                                                                                                                                                                                                                | `src/vocab.ts:292-308`, `src/vocab.ts:424-442`; empirical runs below; spec invariant `docs/cli-schema.md:190`; Go fail-closed: `internal/vocab/sync.go` `LoadIndex`                                                                                                                                                    |
| S1  | should-fix | Non-`VoxError` exceptions escape the closed error-code set as argc `RUNTIME_ERROR` with no `code:` key; Go wrapped every unknown error into `api_error` at `report()`                                                                                                                                                                                                                                  | `src/cli-error.ts:41-48`; `node_modules/argc/src/cli.ts:97-109`; concrete paths: `src/vocab.ts:418` (`saveIndex` in the create path), `src/config.ts:61-68` (corrupt `config.json` → raw `SyntaxError`; Go `config.Load` ignored unmarshal errors and degraded to unauthenticated); Go catch-all: `main.go` `report()` |
| S2  | should-fix | Repeated `--lang` silently keeps only the last value: `vox hear --file x --lang zh --lang en` parses to `lang: "en"`. Go `-l zh -l en` accumulated `["zh","en"]`. Changes both recognition input and the run digest, with no warning                                                                                                                                                                   | `src/schema.ts:8` (`stringOrStrings` union), `src/schema.ts:50`; empirical run below; argc `setField` accumulates only for `kind === 'array'` (`node_modules/argc/src/human.ts:44-58`)                                                                                                                                 |
| S3  | should-fix | `syncVocabulary`'s recovery catch is too wide: failures of `updateVocabulary`, `awaitVocabulary`, or `saveIndex` inside the entry branch are treated as "stored list is gone; recreating" and fall through to `createVocabulary` — a transient update failure creates a second remote list and orphans the old id inside the 10-slot account quota. Go aborts with `api_error` on update/await failure | `src/vocab.ts:376-399` (catch at 394 encloses 377-393); Go: `internal/vocab/sync.go` `Sync` (update errors `return nil, apiError(err)`; only `verifyBinding` failure falls through)                                                                                                                                    |
| S4  | should-fix | The exit-code contract is silently dropped: Go exited 1/2/3 by code class (`internal/voxerr/voxerr.go` `ExitCode`); argc exits 1 for every failure (`node_modules/argc/src/cli.ts:100,108`). Neither the rewritten spec nor `src/SKILL.md` mentions exit codes, so an agent ported from the Go contract branches wrong                                                                                 | `git show cea3992:docs/cli-schema.md` ("Exit codes: 0 · 1 · 2 · 3"); rewritten `docs/cli-schema.md` has no exit-code section                                                                                                                                                                                           |
| S5  | should-fix | `say` no longer streams playback (all PCM chunks are buffered and played only after the stream completes) and the Go-side 2-minute stream timeout is gone — a hung websocket hangs `vox say` forever. The spec carries `say` over "as-is"                                                                                                                                                              | `src/tts.ts:64-99` (buffer at 83/89, play at 99); `src/realtime.ts:14-66` (no timeout); Go: `cmd/say.go` (`player.Write` per chunk inside the callback; `context.WithTimeout(…, 2*time.Minute)`)                                                                                                                       |
| N1  | nit        | `goJSON` reproduces Go's `&<>` escaping but not U+2028/U+2029, so an args value or hotword containing either code point diverges from Go's digest/content hash. Unreachable for realistic args (model/format are fixed, vocab is hex, lang is a language code), but the comment claims parity                                                                                                          | `src/go-json.ts:7-12`; empirical divergence below                                                                                                                                                                                                                                                                      |
| N2  | nit        | On a cache miss with `--vocab`, resolve warnings print twice: once at load (`src/hear.ts:64`) and again from the sync result (`src/hear.ts:90`). Go printed them once (`cmd/hear.go` ignores `result.Warnings`)                                                                                                                                                                                        | `src/hear.ts:64,90`                                                                                                                                                                                                                                                                                                    |
| N3  | nit        | Non-integer weights are accepted: `typeof weight === "number"` passes `4.5`, and `validWeight(4.5)` is true, so it is sent to the API and hashed. Go's `map[string]*int` decode made the same file a hard `vocab_not_found` error                                                                                                                                                                      | `src/vocab.ts:249`, `src/vocab.ts:199-201`; Go: `internal/vocab/vocab.go` `File.Words map[string]*int`                                                                                                                                                                                                                 |
| N4  | nit        | `export --output` now prints a YAML receipt (`written`/`format`/`sid`) on stdout; Go printed nothing on stdout ("or nothing, with --output" in the Go spec). Rewritten spec is silent on the with-output case                                                                                                                                                                                          | `src/handlers.ts:176`; Go: `cmd/export.go` + `cmd/session.go` `writeOut`                                                                                                                                                                                                                                               |
| N5  | nit        | Bare `vox cache` is `NOT_A_COMMAND`; Go defaulted it to `cache status` (`default:"withargs"`)                                                                                                                                                                                                                                                                                                          | `src/schema.ts:192-200`; Go: `cmd/cache.go`                                                                                                                                                                                                                                                                            |
| N6  | nit        | `voice.record` sample texts shrank from 17 languages to 3; a detected `ko`/`de`/… locale now reads the English sample                                                                                                                                                                                                                                                                                  | `src/tts.ts:138-142` vs `cmd/voice.go` `sampleTexts`                                                                                                                                                                                                                                                                   |

### Captured evidence

**M1 — index shape.** TS probe (Bun, calling working-tree `loadIndex` +
`claimedIds` on a temp `~/.vox`-shaped dir):

```
content=[] -> loadIndex OK (no vocab_index_corrupt), claimed ids = []
content="oops" -> loadIndex OK (no vocab_index_corrupt), claimed ids = []
content=123 -> loadIndex OK (no vocab_index_corrupt), claimed ids = []
content={"name": "not-an-object"} -> loadIndex OK (no vocab_index_corrupt), claimed ids = []
```

Go probe (go1.26.6, `json.Unmarshal` into the `Index`/`Entry` types copied
from `cea3992:internal/vocab/sync.go`):

```
content=[] -> err=json: cannot unmarshal array into Go value of type main.Index
content="oops" -> err=json: cannot unmarshal string into Go value of type main.Index
content=123 -> err=json: cannot unmarshal number into Go value of type main.Index
content={"name": "not-an-object"} -> err=json: cannot unmarshal string into Go value of type map[string]main.Entry
```

In Go every one of these is `vocab_index_corrupt`. In TS every one loads,
`claimedIds` returns `{}`, and `pruneVocabularies` (`src/vocab.ts:433-439`)
then marks every remote list an orphan and deletes it unless `--dryRun` was
passed. The existing test (`src/vocab.test.ts:127-132`) covers only invalid
_syntax_ (`"{not json"`), which is exactly the class this hole is not in.

**S2 — repeated `--lang`.** Probe through argc's `parseHumanArgs` with the
working-tree `hear` command schema:

```
["--file","x.wav","--lang","zh","--lang","en"] -> {"file":"x.wav","lang":"en"} langs: ["en"]
["--file","x.wav","--lang","zh"] -> {"file":"x.wav","lang":"zh"} langs: ["zh"]
```

The union `string | string[]` gives the field a non-array descriptor, so
argc's `setField` overwrites instead of accumulating. Multi-hint input is
still reachable via the object form (`vox hear "{file:…, lang:['zh','en']}"`),
but the flag form silently loses data. Fixing the schema to a plain array
restores accumulation but breaks `lang: 'zh'` in object input, since schema
validation runs before `normalizeLangs` — the finding is the silent drop;
which side to keep is a design call.

**Q2 evidence — digest parity.** Go program (go1.26.6) with `Args`/`Hotword`
structs copied verbatim (field order + tags) from `cea3992:internal/run/run.go`
and `cea3992:internal/dashscope/vocabulary.go`, vs Bun running working-tree
`digestOf`/`contentHash`. Audio bytes `"fake audio bytes"`:

| Case                                                                                 | canonical JSON                                                                             | Go sid         | TS sid                         |
| ------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------ | -------------- | ------------------------------ |
| `{Model:"fun-asr", Lang:["zh"]}`                                                     | `{"model":"fun-asr","format":"","lang":["zh"]}`                                            | `89b024754e68` | `89b024754e68`                 |
| `{Model:"fun-asr", Format:"m4a", Speakers:true, Vocab:"8e74bef2", Lang:["zh","en"]}` | `{"model":"fun-asr","format":"m4a","speakers":true,"vocab":"8e74bef2","lang":["zh","en"]}` | `bbca6e3c4d49` | `bbca6e3c4d49`                 |
| `{Model:"qwen-audio-3.0-asr-flash-filetrans", Format:"wav"}`                         | `{"model":"qwen-audio-3.0-asr-flash-filetrans","format":"wav"}`                            | `43d30fc04723` | `43d30fc04723`                 |
| lang `a<b&c>d` (HTML escapes)                                                        | `{"model":"fun-asr","format":"mp3","lang":["a<b&c>d"]}`                                    | `ea776a85ef19` | `ea776a85ef19`                 |
| lang `x y`                                                                           | Go escapes ` `, TS emits it raw                                                            | `08a173e840d8` | `e574d45d97c9` (diverges — N1) |

Content hashes: `[{百炼,5,zh},{赛德克巴莱,4,zh}]` → `e0c35e8a` both sides;
`[{AT&T,5},{<tag>,4}]` → `ba8368ca` both sides; a hotword containing U+2028 →
Go `f88e17b2` vs TS `d1975604` (N1). Full digests, not just the 12-hex
prefixes, matched in every non-U+2028 case. The repo's own anchor constant
`89b024754e68` (`src/run-store.test.ts:48`) equals the Go value.

`bun test`: 37 pass, 0 fail, 5 files.

### Answers to the work-order questions

1. **Spec divergences: adaptation or drop?** Documented adaptations: dotted
   command renames, `auth.login` without the `dashscope` positional, the
   `status` disclosure shape (no masked key), `error: DOMAIN_ERROR` +
   `detail` envelope, `$vox` → `$hints`, `lang` widened to
   `string | string[]`, `say --output` no longer playing (stated in
   `src/SKILL.md:97`). Silent drops: exit codes (S4), repeated-flag
   accumulation (S2), streaming playback + stream timeout (S5), the
   `export --output` stdout receipt (N4), bare `vox cache` (N5), sample-text
   languages (N6). Full list in §3.
2. **sid byte-compatibility.** Proven for realistic inputs, including field
   order, omitempty, and `&<>` HTML escaping — table above. Falsified only
   for U+2028/U+2029 inside an args string (Go escapes them,
   `JSON.stringify` does not), which no realistic `model`/`format`/`vocab`/
   `lang` value contains (N1).
3. **Vocab hashes and `.index.json`.** Content hashes byte-compatible
   (including omitempty `lang` and HTML escapes, table above); resolve order
   matches Go's `sort.Strings` via byte-wise `sortUtf8`
   (`src/go-json.ts:15-19`). Index layout (`name → model → {vocabulary_id,
content_hash, synced_at}`) is unchanged and Go-written `synced_at`
   timestamps parse fine. The compatibility break is M1: shape-corrupt
   indexes load instead of failing.
4. **Export stdout.** Yes: without `output` the handler returns the rendered
   string and argc prints strings raw (`node_modules/argc/src/render.ts:118`),
   so stdout is the document itself. `session.list` reads only `meta.yaml`
   (`src/run-store.ts:159`) — no `text`/`preview` can appear in the index.
   With `output` set, stdout now carries a YAML receipt instead of nothing
   (N4).
5. **Command/flag inventory.** Every Go command has an argc equivalent:
   `auth login/logout/status`, `hear`, `session ls/rm`, `export`,
   `vocab ls/sync/prune`, `say`, `voice list/record/delete`
   (→ `voice.remove`), `cache status/clear`, plus the new top-level
   `status`. Kong short flags (`-m -l -v -f -n -o -s -i -d`) have no argc
   equivalent — the new spec redefines the surface as long flags and object
   input, so this is an adaptation, not a drop. The behavioral flag drops
   are S2 (repeated `--lang`) and N5 (bare `cache`).
6. **Error codes.** The closed set is identical — the same twelve strings
   (`src/cli-error.ts:3-15` vs `internal/voxerr/voxerr.go`). The envelope
   renames `message` → `detail` and adds `error: DOMAIN_ERROR`, both
   documented in the rewritten spec and SKILL, so an agent branching on
   `code:` keeps working. Two leaks: unwrapped exceptions surface as
   `RUNTIME_ERROR` with no `code` at all (S1), and exit codes collapse to 1
   (S4).
7. **Tests.** Untested behavior that matters: `syncVocabulary` end-to-end
   (quota refusal, the recreate path of S3, mismatch propagation),
   `loadIndex` shape validation (the M1 hole — the corruption test covers
   syntax only), the envelope fold threshold (`$hints` vs inlined `text`),
   the handler layer (`export --output`, `session.remove --all` guard),
   `rewriteSayTextArgv`, and everything behind the network/TTY boundary
   (`realtime.ts`, `tts.ts`, `prompt.ts` — `AGENTS.md` declares these
   by-hand). Test that cannot fail: the 20-iteration stability loop in
   `src/vocab.test.ts:93-95` — JS object key iteration is deterministic, so
   the map-order instability the Go original guarded against does not exist
   here; the only load-bearing cross-implementation check is the single sid
   constant at `src/run-store.test.ts:48`, which I verified equals the Go
   value.

## 3. Spec deltas vs `cea3992`

Justified adaptations, documented in the rewritten spec and/or SKILL:

- Dotted commands: `session ls/rm` → `session.list/remove`, `vocab ls` →
  `vocab.list`, `voice delete` → `voice.remove`.
- `auth login dashscope` → `auth.login` (single service; positional gone).
- `auth status` no longer prints a masked key; new top-level `status`
  preflight returning `{authenticated: false}` or `{service: dashscope}`.
- Error envelope is the argc domain envelope: `error: DOMAIN_ERROR`, `code`,
  `detail`, `hint` — `message` renamed to `detail`.
- Folded-envelope key `$vox` → `$hints`; the fold hint now names the argc
  form (`vox export --sid … --format md`).
- `hear.lang` accepts `string | string[]` (Go: repeated `-l` only).
- `say --output` writes the WAV without playing (Go played and wrote;
  documented at `src/SKILL.md:97`).
- `vocab_model_mismatch` hint now names the vendor alias
  (`--model fun`, `src/vocab.ts:455-462`); Go hinted the raw model id
  (`--model fun-asr`), which was not a valid `-m` value.
- Speaker labels on split sentences: the first cue of _each_ split sentence
  is prefixed (`src/export.ts:131-139`, marked deliberate in a comment); Go
  prefixed only the first cue of the whole file. Diarized `srt`/`vtt`/`md`
  output differs from Go for the same stored run.
- Mic enrollment shells out to `ffmpeg` instead of linking miniaudio via
  cgo (`.agents/backlog.md` records this).

Silent drops (no spec or SKILL note) — these are findings S2, S4, S5, N4,
N5, N6 above, plus:

- The Go spec's exit-code table (`0 · 1 usage · 2 API/IO · 3 not found`) has
  no successor statement; argc exits 1 on every failure (S4).
- The closed error set is now closed only for anticipated failures; unknown
  exceptions render as `RUNTIME_ERROR` (S1).

## 4. What I did not verify

- Any live DashScope behavior: upload policy and OSS form POST, the
  transcription task lifecycle, vocabulary create/update/query/delete
  semantics, the TTS websocket protocol, voice enrollment. The TS client
  was compared textually against the Go client (request bodies, headers,
  paths, and response parsing match), not against the API.
- Playback and microphone capture (`afplay`/`ffplay`/`aplay`/`ffmpeg`
  paths in `src/audio.ts`) — read only.
- A real Go-era `~/.vox` store round-trip. Layout, field names, and
  serialization were compared by construction (Go struct tags vs TS
  readers/writers) and by the digest proofs, not against an actual store
  produced by the Go binary.
- Sort ordering of a mixed store: Go wrote nanosecond-precision `created`
  timestamps, TS writes millisecond ISO strings, and `listRuns` compares
  strings (`src/run-store.ts:167-169`) — sub-second inversions within the
  same second are possible and untested.
- `install.sh` and both GitHub workflows were read, not executed.
- argc internals beyond the paths probed (`parseHumanArgs` flag handling,
  `renderResult`, error rendering, exit codes).
