---
type: Guide
title: vox usage
description: Agent-facing contract for transcription, speaker identity, and speech synthesis.
---

# vox

File transcription and TTS through Alibaba Model Studio, with local MLX speaker identification. `vox hear`
uploads a file, transcribes it, and stores a **run**; markdown, subtitles, and
plain text are exports from that run. There is no realtime ASR and no
microphone capture for transcription. Files up to 12 hours are ordinary input.

Do not invent commands. `vox @schema` is the surface.

## First moves

```bash
vox @schema
vox status
```

Unauthenticated status is only `{ authenticated: false }`. Authenticated status
lists the configured provider or providers — never token material.

```bash
vox auth.login                  # TTY: prompts for the key
vox auth.login --token sk-...   # non-TTY
vox auth.login --service mimo   # Xiaomi MiMo API
MIMO_API_KEY=... vox auth.login --service mimo  # safe environment injection
```

## Speech to text

```bash
vox hear --file recording.wav
vox hear --file lecture.mp3 --lang zh
vox hear "{ file: 'lecture.mp3', lang: ['zh'] }"
vox hear "{ file: 'meeting.m4a', speakers: true, vocab: 'meeting' }"
vox hear "{ file: 'recording.wav', refresh: true }"

vox session.list
vox session.list --file recording.wav
vox session.remove --sid a3f1c2
vox session.remove --all

vox export --sid a3f1c2 --format md
vox export --sid a3f1c2 --format srt --output out.srt
vox export --sid a3f1c2 --format txt
```

`--lang` is the cheapest quality win on known-language audio. `--vocab` only
biases domain terms; it cannot recover bad audio. Do not split a file to work
around length: splitting truncates words at every cut.

Vendors: `fun` (default, Fun-ASR), `qwen`, or `mimo`. DashScope's `fun` and
`qwen` cap at **12 hours / 2 GB**. MiMo accepts only MP3/WAV with Base64 payloads
up to 10 MB, returns plain text without timestamps or speaker labels, and does
not support Vox vocabularies. Use it for short A/B samples:

```bash
vox hear --file sample.mp3 --model mimo --lang zh
```

Expect roughly a minute per 45 minutes of audio from the DashScope file models.

`hear` always writes a Markdown transcript under `~/.vox/runs/<sid>/transcript.md`
and returns its path. Pass `output` to choose a destination.

## Speaker identity (Apple Silicon)

Prerequisites: `uv` and `ffmpeg` on PATH (`brew install uv ffmpeg`). ASR still
uses DashScope; voice matching runs locally. The first enrollment or identity
request lazily downloads the pinned model (~26 MB) and Python runtime dependencies.
Weights live in `~/.vox/models/`, runtime files in `~/.vox/runtime/`, and download
caches in `~/.vox/cache/uv` and `~/.vox/models/huggingface`. No PyTorch is required.

Enroll one clean solo recording per person (3–30 seconds; longer files use the
first 30 seconds by default). `start` and `end` select seconds in an existing
recording. Both enrollment and matching remove long near-digital silence before
feature extraction; at least 3 seconds must remain. This is an energy gate, not
a speech/noise classifier. Keep enrollment and evaluation recordings separate when checking quality.

```bash
vox speaker.enroll "{ id: 'ethan', name: 'Ethan', file: 'ethan.wav' }"
vox speaker.enroll "{ id: 'deniffer', name: 'Deniffer', file: 'deniffer.wav', start: 10, end: 30 }"
vox speaker.list
vox hear "{ file: 'meeting.m4a', identify: true, output: 'meeting.transcript.md' }"
```

`identify: true` automatically enables ASR speaker separation and matches against
all enrolled people. Limit candidates with `speakerIds: ['ethan', 'deniffer']`.
MiMo cannot identify speakers because it has no speaker labels.

The Markdown includes timestamps and names. Anonymous IDs remain in the stored
JSON; names and decisions are saved separately as `identified.json`. Uncertain
matches keep `Speaker <id>`, rather than forcing a name. IDs are local to each
recording; multiple IDs may match one person. Identity defaults (`threshold: 0.65`,
`margin: 0.1`) are starting points, not a calibrated accuracy guarantee.

```bash
# Re-identify a stored run after enrollment/config changes; no ASR upload or charge.
vox speaker.identify "{ sid: 'a3f1c2', output: 'meeting.transcript.md' }"
vox speaker.config
vox speaker.config "{ threshold: 0.7, margin: 0.1 }"
vox speaker.enroll "{ id: 'ethan', name: 'Ethan', file: 'better.wav', replace: true }"
vox speaker.remove --id ethan
```

`export` uses the latest saved identity snapshot for that run, including for
SRT/VTT/TXT/JSON. Re-run `speaker.identify` to apply changes to the speaker library.
If identity fails after ASR, the anonymous ASR run remains stored; use
`session.list` and `speaker.identify` to resume. No meeting transcript is modified
outside vox's own run or the explicit output destination.

Profiles and matching settings are in `~/.vox/speakers.json` (private permissions).
Source enrollment audio is not copied; only its normalized voiceprint is stored.
Treat these profiles as personal voice data. `voice.*` is still TTS voice cloning;
`speaker.*` is recognition identity. `VOX_HOME` overrides the state directory for
isolated tests. All speaker artifacts use the model version recorded with the
profiles; changing embedding models requires re-enrollment.

## Result envelopes

`hear` returns a YAML envelope. A short transcript is inlined as `text`. A long
one folds to `preview` plus `$hints` naming the `vox export` command. Do not
try to widen the envelope — run the export.

`session.list` is the same records without `text` or `preview`. `export`
without `--output` prints the document itself (not a YAML wrapper).

## Vocabularies

A vocabulary is a YAML file you write directly. There is no create/edit command.

```yaml
# ~/.vox/vocabulary/meeting.yaml
lang: zh
default_weight: 4
words:
  百炼: 5
  Fun-ASR: 5
  赛德克巴莱:
```

```bash
vox vocab.list
vox vocab.prune --dryRun
```

`vox hear --vocab meeting` syncs on demand. Editing the YAML changes the run
id, so the next `hear` re-recognizes.

Account cap: **10 lists shared across models**. One vocabulary used with both
models consumes two slots. Words are at most 15 characters when non-ASCII.
Weight is 1–5; 50 is a super hotword that only `qwen` honours.

## Text to speech

```bash
vox say --text 'Hello world' --voice Cherry
vox say --text - <<'TXT'
Slow and clear.
TXT
vox say --text 'Save this' --output ~/Desktop/out.wav
vox voice.list
vox voice.record --file ~/sample.wav --name myvoice
vox voice.remove --voiceId <id>
```

System voices: Cherry, Ethan, Chelsie, Serena (zh/en), Dylan (Beijing), Jada
(Shanghai), Sunny (Sichuan). `--output` writes a WAV and does not play.

## Failures

Domain refusals carry `error: DOMAIN_ERROR` and a matchable `code`. Every
failure exits 1 — branch on `code`, not the process status or the detail prose:

`not_authenticated` · `audio_too_large` · `audio_unsupported` ·
`vocab_not_found` · `vocab_quota_exceeded` · `vocab_model_mismatch` ·
`vocab_index_corrupt` · `session_not_found` · `session_ambiguous` ·
`invalid_usage` · `io_error` · `api_error` · `speaker_not_found` ·
`speaker_labels_missing` · `speaker_runtime_unavailable`

`not_authenticated` → `vox auth.login`. `session_not_found` → `vox session.list`.

## Anti-patterns

| Don't                                   | Do instead                            | Why                                            |
| --------------------------------------- | ------------------------------------- | ---------------------------------------------- |
| Split a long file into clips            | Pass the whole file to `hear`         | Cuts drop words and native sentence boundaries |
| Re-read `hear` output for the full hour | `vox export --sid <sid> --format md`  | The envelope folds on purpose                  |
| Treat the envelope as a subtitle file   | `export --format srt` or `txt`        | Index and content never share stdout           |
| Invent a create/edit vocab command      | Write `~/.vox/vocabulary/<name>.yaml` | The YAML is the source of truth                |
| Put multi-line TTS in a JSON object     | `vox say --text - <<'TXT'`            | Escaping burns tokens and breaks               |

## Feedback

File issues at [celados/vox](https://github.com/celados/vox/issues) with the
command, the run id, and the YAML error document.
