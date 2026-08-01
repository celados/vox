# vox

Agent-facing speech to text and text to speech, over Alibaba Model Studio
(DashScope).

Transcription is content-addressed: the same audio and the same recognition
options always resolve to the same run id, and everything after transcription —
markdown, subtitles, plain text — is an operation on the stored run. The full
surface contract lives in [docs/cli-schema.md](docs/cli-schema.md).

## Install

```bash
go install github.com/celados/vox@latest
```

`ffmpeg`/`ffprobe` are optional: `ffprobe` supplies the duration precheck for
non-WAV input, `ffmpeg` compresses the TTS cache.

## Quick Start

```bash
vox auth login dashscope           # prompts, validates, then stores the key

vox hear meeting.wav               # → YAML envelope with the run id
vox export a3f1c2 --format srt     # → subtitles from the stored run

vox say "Hello world" --voice Cherry
```

## Speech to text

```bash
vox hear recording.wav                     # transcribe a file
vox hear recording.wav --vocab meeting     # with a hotword vocabulary
vox hear recording.wav --lang zh           # pin the language
vox hear recording.wav --refresh           # re-recognize, overwrite the run
vox hear --mic --duration 10               # record and transcribe

vox session ls                             # the run index, newest first
vox session ls --file recording.wav        # runs for one source
vox session rm a3f1c2                      # accepts any unique prefix

vox export a3f1c2 --format md              # markdown with frontmatter
vox export a3f1c2 --format srt -o out.srt  # subtitles
vox export a3f1c2 --format json            # transcript with word timestamps
```

`hear` prints one YAML document. A short transcript is inlined as `text`; a long
one folds to a `preview` plus the `vox export` command that reads the rest, so a
caller never receives an unbounded transcript it did not ask for.

```yaml
sid: fe3ddc6ebfca
source: ~/recordings/meeting.wav
model: fun-asr-flash-2026-06-15
vocab: meeting@e4a2648e
created: 2026-08-01T06:08:57Z
path: ~/.vox/runs/fe3ddc6ebfca
size: { tokens: 54, words: 37, chars: 65, duration: 10 }
text: 第一句话，我们在测试句子切分…
```

Failures print one YAML document to stderr with a closed-set `code`, so a caller
branches instead of parsing prose:

```yaml
code: audio_too_large
message: audio is 412s, over the 300s limit for this model
hint: split the file into segments under 5 minutes
```

### Models

| Model | Hotword lists | Super hotwords | Language hints |
|-------|---------------|----------------|----------------|
| `fun-asr-flash-2026-06-15` (default) | yes | no | first one only |
| `qwen-audio-3.0-asr-flash` | yes | yes (weight 50) | up to 4 |

Both cap at 5 minutes / 10MB per request, checked locally before upload. Both
cover Mandarin plus major dialects and ~30 other languages.

## Vocabularies

A vocabulary is a YAML file. The CLI never edits it — it only reconciles it with
the server-side hotword list.

```yaml
# ~/.vox/vocabulary/meeting.yaml
lang: zh
default_weight: 4
words:
  百炼: 5
  Fun-ASR: 5
  赛德克巴莱:           # empty → default_weight

# Optional: per-model intent. Merged over the base, model block wins.
models:
  fun-asr-flash-2026-06-15:
    words:
      声网: 5
```

```bash
vox vocab ls                  # names, paths, per-model sync state, remote quota
vox vocab sync meeting        # usually unnecessary — `hear --vocab` syncs on demand
vox vocab prune --dry-run     # server-side lists no local file claims
```

Sync is driven by a content hash: unchanged content makes no request, changed
content updates in place so the `vocabulary_id` survives. Because the resolved
content is part of the run id, editing the YAML produces a new run on the next
`hear` with no cache-busting flag.

Model-specific API limits are applied by the adapter, not by you: `weight: 50`
is clamped to 5 on Fun-ASR, unsupported language codes are dropped, oversized
words are skipped, and each is reported on stderr. **The account cap is 10 lists
shared across all models** — one vocabulary synced to both models consumes two.

## Text to speech

`say`, `voice` and `cache` carry over unchanged and have not been moved onto
runs yet.

```bash
vox say "你好世界" --voice Cherry --speed 1.2
vox say "Save this" --output out.wav
vox voice list
vox voice record --file sample.wav --name myvoice
vox cache status
```

| Voice | Gender | Language |
|-------|--------|----------|
| Cherry | Female | zh/en |
| Ethan | Male | zh/en |
| Chelsie | Female | zh/en |
| Serena | Female | zh/en |
| Dylan | Male | zh (Beijing) |
| Jada | Female | zh (Shanghai) |
| Sunny | Female | zh (Sichuan) |

## Storage

```
~/.vox/
  config.json          credentials (0600)
  runs/<sid>/          meta.yaml · audio.<ext> · run.json
  vocabulary/          <name>.yaml + .index.json
  cache/               TTS audio
```

## License

MIT
