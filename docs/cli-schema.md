---
type: Spec
title: vox CLI surface
description: >
  The command surface, output contracts, and on-disk data model for vox as an
  agent-facing STT/TTS tool. Sessions are content-addressed; everything after
  transcription is a session operation.
status: draft # draft | accepted | superseded
version: 0.1
generated: { by: claude/opus-5, at: 2026-08-01T00:00:00Z }
---

# vox CLI surface

The implementation is API calls. The design work is here: what the commands
are, what they print, and what state they leave behind.

Three invariants the whole surface is built on:

1. **Content-addressed sessions.** `sid = short_hash(audio, recognition args)`.
   The same input always yields the same `sid`, and a repeated call costs zero
   API requests.
2. **One transcription, many exports.** Recognition happens once. Markdown,
   subtitles, and plain text are pure functions over a stored session.
3. **One thing on stdout.** Every command writes exactly one machine-consumable
   artifact to stdout; all human-facing progress goes to stderr.

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
   * Transcribe audio into a session. Idempotent: an existing sid returns the
   * stored result without calling the API.
   *
   * stdout: transcript text (default) · sid (--sid) · full run JSON (--json)
   *
   * @example
   * vox hear meeting.m4a
   * vox hear meeting.m4a --vocab meeting --sid
   * vox hear meeting.m4a --lang zh --json
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
    sid?: boolean          // print only the session id
    json?: boolean         // print the full run record
    noCache?: boolean      // force re-recognition, overwriting the stored run
  })

  /** Inspect and manage stored sessions. */
  session: {
    /**
     * List sessions, newest first. --file filters to one source path.
     *
     * stdout: TSV — sid, created, model, duration, source
     *
     * @example
     * vox session ls
     * vox session ls --file meeting.m4a
     * vox session ls --json
     */
    list(input: { file?: string; limit?: number; json?: boolean })
    /**
     * Show one session. Accepts a unique sid prefix.
     *
     * @example
     * vox session show a3f1c2
     * vox session show a3f1c2 --json
     */
    show(input: { sid: string; json?: boolean })
    /**
     * Print the session directory path so an agent can read the files directly.
     *
     * @example
     * vox session path a3f1c2
     */
    path(input: { sid: string })
    /**
     * Delete sessions.
     *
     * @example
     * vox session rm a3f1c2
     * vox session rm --all
     */
    remove(input: { sid?: string; all?: boolean })
  }

  /**
   * Render a stored session. Subtitle segmentation is computed client-side from
   * word timestamps and punctuation — the API returns the whole take as one
   * sentence, so the segmentation knobs live here.
   *
   * stdout: the rendered document (or nothing, with --output)
   *
   * @example
   * vox export a3f1c2 --format srt
   * vox export a3f1c2 --format srt --max-chars 24 --output meeting.srt
   * vox export a3f1c2 --format md
   */
  export(input: {
    sid: string
    format: "srt" | "vtt" | "md" | "txt" | "json"
    output?: string        // write to a file instead of stdout
    maxChars?: number      // subtitle line budget (srt/vtt, default 28)
    maxDuration?: number   // max seconds per cue (srt/vtt, default 6)
    gapMs?: number         // inter-word pause that forces a split (default 400)
  })

  /**
   * Hotword lists. The YAML files under ~/.vox/vocabulary/ are the source of
   * truth; these commands only reconcile them with the server side. There is no
   * create/edit command — write the YAML file.
   */
  vocab: {
    /**
     * List local vocabularies with per-model sync state and remote quota usage.
     *
     * stdout: TSV — name, words, models synced, drift
     *
     * @example
     * vox vocab ls
     */
    list(input: { json?: boolean })
    /** Show one vocabulary's resolved words and its server-side ids. */
    show(input: { name: string; json?: boolean })
    /**
     * Print the YAML path for a vocabulary so an agent can write it directly.
     * Prints the directory when name is omitted.
     *
     * @example
     * vox vocab path meeting
     */
    path(input: { name?: string })
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

  /**
   * Speak text with TTS. Streams to the speaker, or writes a file with --output.
   *
   * @example
   * vox say "Hello world" --voice Cherry
   * vox say "你好世界" --output greeting.wav
   */
  say(input: {
    text: string
    voice?: string
    lang?: string
    instruct?: string      // expressive style; system voices only
    speed?: number         // 0.5-2.0, default 1.0
    output?: string
    noCache?: boolean
  })

  /** Manage cloned voice profiles. */
  voice: {
    list(input: { json?: boolean })
    /**
     * Enroll a cloned voice from a file or a live recording.
     *
     * @example
     * vox voice record --file sample.wav --name myvoice
     */
    record(input: { file?: string; name: string; lang?: string; duration?: number })
    remove(input: { voiceId: string })
  }

  /** TTS audio cache. Sessions are managed with `vox session rm`. */
  cache: {
    status()
    clear()
  }
}
```

## Output contract

| Stream | Carries |
| ------ | ------- |
| stdout | Exactly one artifact: transcript, sid, TSV table, or rendered document. Never mixed. |
| stderr | Progress, model/latency lines, warnings, and errors. |

Errors print a single JSON object to stderr and exit non-zero:

```json
{ "code": "vocab_model_mismatch", "message": "...", "hint": "vox vocab sync meeting --model fun-asr-flash-2026-06-15" }
```

Exit codes: `0` success · `1` usage/validation · `2` API failure · `3` not found.

Codes are a closed set so an agent can branch without parsing prose. Initial
set: `not_authenticated`, `audio_too_large`, `audio_unsupported`,
`vocab_not_found`, `vocab_quota_exceeded`, `vocab_model_mismatch`,
`session_not_found`, `session_ambiguous`, `api_error`.

## Data model

```
~/.vox/
  config.json                    credentials (0600)
  state.json                     last-used voice
  sessions/<sid>/
    meta.json                    source path, size, sha256, format, duration, created_at, args
    audio.<ext>                  the source audio, copied in
    run.json                     model, vocabulary snapshot, text, words[], usage
  vocabulary/
    <name>.yaml                  source of truth, hand- or agent-edited
    .index.json                  name → { model → { vocabulary_id, content_hash, synced_at } }
  cache/                         TTS audio (opus)
```

`sid` is the first 12 hex of `sha256(audio_bytes ‖ canonical(args))`, where
`args` is only what changes the transcript: `model`, `lang`, `context`, and the
vocabulary's **content hash** — not its name. Editing a vocabulary YAML
therefore produces a new `sid` on the next run, with no cache-busting flag.

`--json` and `--format` are not part of `args`: presentation never forks a
session.

Commands taking a `sid` accept any unique prefix; an ambiguous prefix is
`session_ambiguous`, not a guess.

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
`create_vocabulary` and poll `query_vocabulary` until `status: OK`. A
`target_model` mismatch fails **silently** server-side, so the adapter refuses
the call locally instead of trusting the API to complain.

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

- **TTS sessions.** `say` could take the same content-addressed treatment
  (`sid = hash(text, voice, params)` → a stable audio path), collapsing the tool
  to a single "stable id + idempotent" concept. Not decided; `say` is specified
  above in its current form.
- **Long audio.** The non-realtime endpoint caps at 5 minutes / 10MB. Meeting
  recordings need either `fun-asr` (12h, async filetrans, a different schema) or
  client-side chunking with session-level merge. The session model can carry the
  intermediate state either way.
- **Qwen retirement.** Kept as a valid `--model` value at zero maintenance cost
  now that both models share one code path. Its only exclusive features are
  inline hotwords (deliberately unused) and super hotwords.
