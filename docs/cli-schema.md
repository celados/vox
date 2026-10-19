---
type: Spec
title: vox CLI surface
description: >
  The command surface, output contracts, and on-disk data model for vox as an
  agent-facing STT/TTS tool. Runs are content-addressed; everything after
  transcription is an operation on a stored run.
status: accepted
version: 1.2
generated: { by: codex, at: 2026-10-16T00:00:00Z }
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

The authoritative types and descriptions are [src/schema.ts](../src/schema.ts).
Use `vox @schema`, or `vox @schema .speaker` for local identity commands.
The embedded usage guide is [src/index.md](../src/index.md), served by `vox @skill`.

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

## Speaker identity

`hear` writes a Markdown transcript and returns its path; `output` chooses the
file destination. `identify: true` implies `speakers: true`, then matches selected
clean 3–10 second excerpts against enrolled profiles with a local MLX model.
`speaker.enroll` accepts a stable ID, display name, and 3–30 seconds of solo audio.
`speaker.config` controls minimum cosine similarity and candidate margin.

Identity is a separate projection, never part of the ASR cache key. Original
`sentence.speaker` stays unchanged; `speaker_identity` and `speaker_name` annotate
matches. `result.identification` records the model, profile fingerprint, thresholds,
matching time, and per-speaker decisions. Unknown/ambiguous or internally
inconsistent clusters keep anonymous labels. Multiple clusters may identify one
person. Threshold defaults require calibration on actual meeting audio.

`speaker.identify` works on a stored run, without cloud credentials or another ASR
request. `export` uses the latest saved identity projection when present; profile
changes do not retroactively rewrite an existing snapshot. Re-identify explicitly.
If local identity fails, the ASR run remains saved and recoverable.

The runtime and model download lazily under `~/.vox`; `uv` and `ffmpeg` are host
prerequisites on Apple Silicon macOS. The model source is pinned and SHA256-verified;
conversion only reads ONNX tensor data, never remote code. Runtime dependencies are
locked; bundles embed the Python helper and dependency lock. `VOX_HOME` overrides
the state directory for isolated execution.

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
`session_ambiguous`, `io_error`, `api_error`, `speaker_not_found`,
`speaker_labels_missing`, `speaker_runtime_unavailable`.

Failures exit 1. Branch on `code`, not a numeric exit class.

An index command emits `[]` rather than nothing when empty.

## Data model

```
~/.vox/
  config.json
  state.json
  speakers.json
  models/
    wespeaker-resnet34-lm-v1/
    huggingface/
  runtime/<helper-hash>/
  runs/<sid>/
    meta.yaml
    audio.<ext>
    run.json
    transcript.md
    identified.json     # optional local identity projection
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
