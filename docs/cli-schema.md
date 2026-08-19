---
type: Spec
title: vox CLI surface
description: >
  The command surface, output contracts, and on-disk data model for vox as an
  agent-facing STT/TTS tool. Runs are content-addressed; everything after
  transcription is an operation on a stored run.
status: accepted
version: 1.1
generated: { by: grok/grok-4.6, at: 2026-08-19T00:00:00Z }
---

# vox CLI surface

argc v7 dotted commands on Bun. `vox @schema` is the live contract.

Four invariants the whole surface is built on:

1. **Content-addressed runs.** `sid = short_hash(audio, recognition args)`. The
   same input always yields the same `sid`, and a repeated call costs zero API
   requests. `sid` is something commands _return_, never something you pass in
   to select an output mode.
2. **One transcription, many exports.** Recognition happens once. Markdown,
   subtitles, and plain text are pure functions over a stored run.
3. **Index out, content out — never both.** `hear`, `session.list` and
   `vocab.list` emit a YAML index on stdout. `export` without `--output` emits
   the document itself. Human progress goes to stderr.
4. **One path: upload, infer, output.** vox is an offline file transcriber.
   There is no realtime ASR and no microphone capture for transcription.

## Schema

```ts
type Vox = {
  /** Preflight: { authenticated: false } or { service: 'dashscope' }. */
  status();

  auth: {
    login(input: { token?: string });
    logout();
    status();
  };

  hear(input: {
    file: string;
    model?: "fun" | "qwen";
    vocab?: string;
    lang?: string[];
    speakers?: boolean;
    refresh?: boolean;
  });

  session: {
    list(input: { file?: string; limit?: number });
    remove(input: { sid?: string; all?: boolean });
  };

  export(input: { sid: string; format: "srt" | "vtt" | "md" | "txt" | "json"; output?: string });

  vocab: {
    list();
    sync(input: { name?: string; all?: boolean; model?: "fun" | "qwen"; force?: boolean });
    prune(input: { dryRun?: boolean });
  };

  say(input: {
    text: string;
    voice?: string;
    lang?: string;
    instruct?: string;
    speed?: number;
    output?: string;
    noCache?: boolean;
  });

  voice: {
    list();
    record(input: { file?: string; name?: string; lang?: string; duration?: number });
    remove(input: { voiceId: string });
  };

  cache: {
    status();
    clear();
  };
};
```

`say`, `voice` and `cache` are the legacy TTS surface. `cache` exists only
because TTS has not been moved onto runs yet.

## Recognition

One endpoint, one shape: upload the file to Model Studio's free temporary
store, submit an async task, poll it, keep the result.

|              |                                                                             |
| ------------ | --------------------------------------------------------------------------- |
| Endpoint     | `audio/asr/transcription`, `X-DashScope-Async: enable`                      |
| Vendors      | `fun` → `fun-asr` (default) · `qwen` → `qwen-audio-3.0-asr-flash-filetrans` |
| Cap          | 12 h / 2 GB                                                                 |
| Segmentation | native, timestamped sentences                                               |
| Per word     | text, timings, punctuation, confidence                                      |
| Extras       | `speakers` diarization, precompiled hotwords, language hints                |
| Latency      | ~1 min per 45 min of audio                                                  |

`file_urls` accepts no local upload and no base64, so every run begins with an
upload: `GET /uploads?action=getPolicy`, a form POST to the returned OSS host,
then an `oss://` URL that lives 48 hours.

**Client-side chunking is not a fallback.** It truncates the word at every cut
and forfeits native segmentation. It is justified only above the 12-hour cap.

## Output contract

| Stream | Carries                                                                                            |
| ------ | -------------------------------------------------------------------------------------------------- |
| stdout | A YAML index (`hear`, `session.list`, `vocab.list`) or a rendered document (`export`). Never both. |
| stderr | Progress, warnings, and errors.                                                                    |

A long transcript folds behind `$hints` plus `preview`. Under the 2000-token
threshold the envelope inlines `text` instead.

Errors are argc domain envelopes on stderr:

```yaml
error: DOMAIN_ERROR
code: vocab_model_mismatch
detail: list vocab-meeting-8e74 was built for qwen-audio-3.0-asr-flash-filetrans, not fun-asr
hint: vox vocab.sync --name meeting --model fun
```

Codes: `invalid_usage`, `not_authenticated`, `audio_too_large`,
`audio_unsupported`, `vocab_not_found`, `vocab_quota_exceeded`,
`vocab_model_mismatch`, `vocab_index_corrupt`, `session_not_found`,
`session_ambiguous`, `io_error`, `api_error`.

Failures exit 1. Branch on `code`, not a numeric exit class.

An index command emits `[]` rather than nothing when empty.

## Data model

```
~/.vox/
  config.json
  state.json
  runs/<sid>/
    meta.yaml
    audio.<ext>
    run.json
  vocabulary/
    <name>.yaml
    .index.json
  cache/
```

`sid` is the first 12 hex of `sha256(audio_bytes ‖ canonical(args))`, where
`args` JSON matches Go `encoding/json` field order and omitempty: `model`,
`format`, optional `speakers`, `vocab` (content hash, not name), `lang`.
Editing a vocabulary YAML therefore produces a new `sid` on the next run.

The **full** digest is stored in `run.json` and checked on every cache hit.

Commands taking a `sid` accept any unique hex prefix. An ambiguous prefix is
`session_ambiguous`. `..` is `session_not_found`, never a path.

## Vocabulary YAML

```yaml
lang: zh
default_weight: 4
words:
  百炼: 5
  Fun-ASR: 5
  赛德克巴莱:

models:
  fun-asr:
    words:
      声网: 5
```

Model-specific API constraints stay in the adapter:

- `weight: 50` is Qwen-only. On Fun-ASR the adapter clamps to 5 and warns.
- Word length: ≤15 characters when any non-ASCII is present; ≤7 space-separated
  segments when pure ASCII.
- Per-word `lang` accepts only `zh`/`en`/`ja` on Fun-ASR.
- Caps: 2000 words per list, 50 super hotwords, **10 lists per account**.

A corrupt index is `vocab_index_corrupt`, never an empty one.

## Open

- **TTS runs.** `say` could take the same content-addressed treatment.
- **Envelope fold threshold.** Written as 2000 tokens.
- **Duration probe on non-WAV input.** WAV is parsed inline; everything else
  needs `ffprobe`.
- **Concurrent `hear` on one input.** Publication is atomic; two processes
  that miss simultaneously both call the API.
