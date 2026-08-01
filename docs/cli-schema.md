---
type: Spec
title: vox CLI surface
description: >
  The command surface, output contracts, and on-disk data model for vox as an
  agent-facing STT/TTS tool. Runs are content-addressed; everything after
  transcription is an operation on a stored run.
status: draft # draft | accepted | superseded
version: 0.3
generated: { by: claude/opus-5, at: 2026-08-01T00:00:00Z }
---

# vox CLI surface

The implementation is API calls. The design work is here: what the commands
are, what they print, and what state they leave behind.

Three invariants the whole surface is built on:

1. **Content-addressed runs.** `sid = short_hash(audio, recognition args)`. The
   same input always yields the same `sid`, and a repeated call costs zero API
   requests. `sid` is something commands *return*, never something you pass in
   to select an output mode.
2. **One transcription, many exports.** Recognition happens once. Markdown,
   subtitles, and plain text are pure functions over a stored run.
3. **Index out, content out — never both.** `hear`, `session ls` and `vocab ls`
   emit a YAML index on stdout. `export` emits the document itself. Human
   progress goes to stderr. There are no `--json` / `--sid` output-mode flags.

## Schema

```ts
type Vox = {
  /** Authenticate against DashScope (Alibaba Model Studio). */
  auth: {
    /**
     * Store the API key. Prompts on a TTY when --token is omitted so the key
     * stays out of shell history. Validated against the API before it is written.
     *
     * @example
     * vox auth login dashscope
     * vox auth login dashscope --token sk-...
     */
    login(input: { token?: string })
    /** Clear stored credentials. */
    logout()
    /** Show configured services with a masked key. */
    status()
  }

  /**
   * Transcribe audio into a run. Idempotent: an existing sid returns the stored
   * record without calling the API. Duration and size are checked locally
   * before upload — over the limit is audio_too_large, not a server rejection.
   *
   * stdout: the run's YAML envelope (see Output contract)
   *
   * @example
   * vox hear meeting.m4a
   * vox hear meeting.m4a --vocab meeting --lang zh
   * vox hear --mic --duration 10
   */
  hear(input: {
    file?: string          // positional; omit with --mic
    mic?: boolean          // record from the default input device
    duration?: number      // --mic only, seconds (default 5)
    model?: string         // fun-asr-flash-2026-06-15 (default) | qwen-audio-3.0-asr-flash
    vocab?: string         // vocabulary name under ~/.vox/vocabulary/<name>.yaml
    lang?: string[]        // language hints; auto-detect when unset
    context?: string[]     // prompt context, max 5 entries / 400 chars per round
    refresh?: boolean      // re-recognize and overwrite the stored run
  })

  /** The stored runs. */
  session: {
    /**
     * The run index, newest first — the same envelope `hear` returns, one entry
     * per run, without the transcript. This is the only way to browse; there is
     * no show/path, because the index carries the path and `export` carries the
     * content.
     *
     * @example
     * vox session ls
     * vox session ls --file meeting.m4a
     */
    list(input: { file?: string; limit?: number })
    /**
     * Delete runs. Accepts a unique sid prefix.
     *
     * @example
     * vox session rm a3f1c2
     * vox session rm --all
     */
    remove(input: { sid?: string; all?: boolean })
  }

  /**
   * Render a stored run. Subtitle cues are segmented client-side from word
   * timestamps and punctuation, because the API returns the whole take as one
   * sentence. Cue length and duration are fixed conventions, not flags.
   *
   * stdout: the rendered document (or nothing, with --output)
   *
   * @example
   * vox export a3f1c2 --format md
   * vox export a3f1c2 --format srt --output meeting.srt
   */
  export(input: {
    sid: string
    format: "srt" | "vtt" | "md" | "txt" | "json"
    output?: string        // write to a file instead of stdout
  })

  /**
   * Hotword lists. The YAML files under ~/.vox/vocabulary/ are the source of
   * truth; these commands only reconcile them with the server side. There is no
   * create/edit command — write the YAML file.
   */
  vocab: {
    /**
     * The vocabulary index: name, path, word count, per-model sync state, and
     * remote quota usage. Same reasoning as `session ls` — the index carries the
     * path, so an agent reads and writes the YAML directly.
     *
     * @example
     * vox vocab ls
     */
    list()
    /**
     * Push local YAML to the server for a target model. Content-hash equal is a
     * no-op; changed content updates in place, keeping the same vocabulary_id.
     * Normally implicit — `vox hear --vocab X` syncs on demand.
     *
     * @example
     * vox vocab sync meeting
     * vox vocab sync --all --model qwen-audio-3.0-asr-flash
     */
    sync(input: { name?: string; all?: boolean; model?: string; force?: boolean })
    /**
     * Delete server-side lists that no local YAML claims. The account cap is 10
     * lists shared across all models, so orphans are expensive.
     *
     * @example
     * vox vocab prune --dry-run
     */
    prune(input: { dryRun?: boolean })
  }

  // --- legacy, unchanged, pending its own redesign ---
  say(input: { text: string; voice?: string; lang?: string; instruct?: string; speed?: number; output?: string; noCache?: boolean })
  voice: { list(); record(input: { file?: string; name: string; lang?: string; duration?: number }); remove(input: { voiceId: string }) }
  cache: { status(); clear() }
}
```

`say`, `voice` and `cache` carry over as-is. `cache` is the degenerate form of a
run — a key→artifact map with no metadata and no index — and it exists only
because TTS has not been moved onto runs yet. When `say` gets the same
content-addressed treatment, the `cache` group disappears into `session`.

## Output contract

| Stream | Carries |
| ------ | ------- |
| stdout | A YAML index (`hear`, `session ls`, `vocab ls`) or a rendered document (`export`). Never both, never mixed. |
| stderr | Progress, model/latency lines, warnings, and errors. |

`hear` returns the run envelope. A short take inlines its transcript; a long one
folds to a preview and tells the caller how to read the rest — the same
threshold behaviour `ctx read` uses, so an agent never eats an unbounded
transcript it did not ask for:

```yaml
---
$vox:
- "Transcript folded at 2000 tokens. Read it with `vox export a3f1c2 --format md`."
---
sid: a3f1c2d4e5f6
source: ~/recordings/meeting.m4a
model: fun-asr-flash-2026-06-15
vocab: meeting@8e74bef2
lang: [zh]
created: 2026-08-01T12:00:00Z
path: ~/.vox/runs/a3f1c2d4e5f6
size: { tokens: 9800, words: 19600, chars: 29400, duration: 1830 }
preview: 第一句话，我们在测试句子切分，第二句话，字幕需要每一句的时间戳…
```

Under the threshold the `$vox` block and `preview` are replaced by a `text`
field carrying the full transcript, so the common short-clip case stays a single
command.

`session ls` is the same records as a YAML list, without `text` or `preview`.

Errors print one YAML document to stderr and exit non-zero:

```yaml
code: vocab_model_mismatch
message: vocabulary "meeting" is synced for qwen-audio-3.0-asr-flash, not fun-asr-flash-2026-06-15
hint: vox vocab sync meeting --model fun-asr-flash-2026-06-15
```

Exit codes: `0` success · `1` usage/validation · `2` API or local I/O failure ·
`3` not found.

Codes are a closed set so an agent can branch without parsing prose:
`invalid_usage`, `not_authenticated`, `audio_too_large`, `audio_unsupported`,
`vocab_not_found`, `vocab_quota_exceeded`, `vocab_model_mismatch`,
`vocab_index_corrupt`, `session_not_found`, `session_ambiguous`, `io_error`,
`api_error`.

Argument parsing errors go through the same renderer as everything else: an
unknown flag is `invalid_usage`, not a wall of usage text.

An index command emits its artifact even when the collection is empty — `[]`
rather than nothing, so a caller never has to distinguish "no results" from "no
output".

## Data model

```
~/.vox/
  config.json                    credentials (0600)
  state.json                     last-used voice
  runs/<sid>/
    meta.yaml                    the envelope: source, args, size, created_at
    audio.<ext>                  the source audio, copied in
    run.json                     model, vocabulary snapshot, text, words[], usage
  vocabulary/
    <name>.yaml                  source of truth, hand- or agent-edited
    .index.json                  name → { model → { vocabulary_id, content_hash, synced_at } }
  cache/                         TTS audio (opus) — legacy, folds into runs/ later
```

`sid` is the first 12 hex of `sha256(audio_bytes ‖ canonical(args))`, where
`args` is only what changes the transcript: `model`, `format`, `lang`, `context`,
and the vocabulary's **content hash** — not its name. Editing a vocabulary YAML
therefore produces a new `sid` on the next run, with no cache-busting flag; two
vocabularies with identical words do not fork one.

Export format is not part of `args`: presentation never forks a run.

The **full** digest is stored in `run.json` and checked on every cache hit. `sid`
is a 48-bit display prefix, so a collision must degrade to a miss rather than
silently serve another input's transcript.

A run is published by renaming a staging directory into place, so a concurrent
`session ls` or `export` never observes a partially written run.

Commands taking a `sid` accept any unique prefix, validated as hex before it
reaches the filesystem — the value flows into `os.RemoveAll`, so `..` must never
survive a path join. An ambiguous prefix is `session_ambiguous`, not a guess, and
`rm <sid> --all` is refused rather than resolved in either direction.

## Vocabulary YAML

```yaml
# ~/.vox/vocabulary/meeting.yaml
lang: zh              # optional; omit to let the model detect
default_weight: 4     # the value Alibaba recommends
words:
  百炼: 5
  Fun-ASR: 5
  赛德克巴莱:          # empty → default_weight

# Escape hatch. Merged over the base, model block wins. Most files omit it.
models:
  fun-asr-flash-2026-06-15:
    words:
      声网: 5
```

Model-specific *API constraints* stay in the adapter, never in this file — an
agent editing a vocabulary should not need to know any model's rules:

- `weight: 50` (super hotword) is Qwen-only. On Fun-ASR the adapter clamps to 5
  and warns on stderr.
- Word length: ≤15 characters when non-ASCII is present; ≤7 space-separated
  segments when pure ASCII.
- Per-word `lang` in the vocabulary API accepts only `zh`/`en`/`ja` on Fun-ASR;
  unsupported codes are dropped rather than rejected.
- Caps: 2000 words per list, 50 super hotwords, **10 lists per account across
  all models**.

Sync is content-hash driven: equal hash is a no-op, changed content calls
`update_vocabulary` so the `vocabulary_id` survives, missing entries call
`create_vocabulary`. Both create and update poll `query_vocabulary` until
`status: OK`, since a list used while `UNDEPLOYED` has no effect and says so
nowhere.

A `target_model` mismatch fails **silently** server-side — the recognition
request succeeds and the hotwords simply do nothing. The stored binding is
therefore verified against the remote list before use and reported as
`vocab_model_mismatch`; a list that has vanished is recreated.

`hear` resolves a vocabulary locally, hashes it, and checks the run store
**before** touching the network. A stored run costs zero API requests even when
`--vocab` is passed.

The index is written through a temp file and rename. A corrupt index is
`vocab_index_corrupt`, never an empty one: treating it as empty would make
`prune` consider nothing claimed and delete every list on the account.

## Verified behaviour

Measured 2026-08-01 against the live API; these decide the shape above.

- **Precompiled vocabularies work on `fun-asr-flash-2026-06-15`.** The model
  list page claims otherwise; the hotword page and the API agree it works.
  Baseline `别连语音识别测试，funASR和昆都要跑通。` → with vocabulary
  `百炼语音识别测试，Fun-ASR和Kun都要跑通。` No context-prompt fallback is needed.
- **The API does not segment sentences on this path.** An 11s four-sentence take
  returns one `sentence` object spanning 120–10640ms, and SSE emits exactly one
  event — streaming buys nothing. Subtitle cues must be derived from
  `words[].punctuation` and inter-word gaps, which is why `export` owns the
  segmentation knobs.
- `create_vocabulary` returned `status: OK` immediately, but the polling loop
  stays: the documented `UNDEPLOYED` state is not contractually excluded.
- The legacy host `dashscope.aliyuncs.com` serves both the recognition and the
  vocabulary APIs; the workspace-specific `maas.aliyuncs.com` domains are a
  performance migration, not a requirement.

Sources:
[非实时语音识别 API](https://help.aliyun.com/zh/model-studio/non-real-time-speech-recognition-for-fun-asr-flash) ·
[定制热词 HTTP API](https://help.aliyun.com/zh/model-studio/vocabulary-http-api) ·
[热词使用与限制](https://help.aliyun.com/zh/model-studio/improve-asr-accuracy) ·
[ASR 模型列表](https://help.aliyun.com/zh/model-studio/asr-model/)

## Open

- **TTS runs.** `say` could take the same content-addressed treatment
  (`sid = hash(text, voice, params)` → a stable audio path), collapsing the tool
  to a single "stable id + idempotent" concept. Not decided; `say` is specified
  above in its current form.
- **Long audio.** The non-realtime endpoint caps at 5 minutes / 10MB, so `hear`
  pre-checks and refuses. That refusal is the honest end state until we pick a
  path: `fun-asr` (12h, async filetrans, a different schema) or client-side
  chunking with run-level merge. The run model carries the intermediate state
  either way.

- **Envelope fold threshold.** Written as 2000 tokens. It only needs to be small
  enough that an agent doing `hear` on an hour of audio does not get 10k tokens
  it did not ask for.

- **Duration probe on non-WAV input.** WAV is parsed inline; everything else
  needs `ffprobe`. Without it the duration is unknown, and `hear` warns and
  enforces only the size cap rather than refusing — failing closed would make
  every mp3 unusable on a machine without ffmpeg.

- **Concurrent `hear` on one input.** Publication is atomic, so the stored run is
  never inconsistent, but two processes that miss simultaneously both call the
  API and pay twice. A per-sid lock would close it; not worth the machinery until
  it happens.
- **Qwen retirement.** Kept as a valid `--model` value at zero maintenance cost
  now that both models share one code path. Its only exclusive features are
  inline hotwords (deliberately unused) and super hotwords.
