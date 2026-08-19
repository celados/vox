# vox

Agent-facing speech to text and text to speech, over Alibaba Model Studio
(DashScope). An [argc](https://github.com/ethan-huo/argc) CLI on Bun.

Offline file transcription: upload, infer, store. There is no realtime ASR
and no microphone capture for transcription — vox transcribes files, up to
12 hours each.

Transcription is content-addressed: the same audio and the same recognition
options always resolve to the same run id, and everything after transcription
— markdown, subtitles, plain text — is an operation on the stored run. The
full surface contract lives in [docs/cli-schema.md](docs/cli-schema.md).

## Install

From a checkout:

```bash
bun install
bun link          # PATH → ./src/main.ts
vox --version
vox @schema
```

From a GitHub Release:

```bash
curl -fsSL https://raw.githubusercontent.com/celados/vox/main/install.sh | bash
```

`ffmpeg`/`ffprobe` are optional: `ffprobe` reports duration for non-WAV input
before upload, `ffmpeg` compresses the TTS cache and records a clone sample.
Playback uses `afplay` on macOS, or `ffplay`/`aplay`.

## Quick Start

```bash
vox auth.login                 # prompts, validates, then stores the key

vox hear --file meeting.wav    # → YAML envelope with the run id
vox export --sid a3f1c2 --format srt

vox say --text 'Hello world' --voice Cherry
```

## Speech to text

```bash
vox hear --file recording.wav
vox hear --file lecture.mp3 --lang zh
vox hear "{ file: 'lecture.mp3', lang: ['zh'] }"
vox hear "{ file: 'meeting.m4a', speakers: true }"
vox hear "{ file: 'recording.wav', vocab: 'meeting' }"
vox hear "{ file: 'recording.wav', refresh: true }"

vox session.list
vox session.list --file recording.wav
vox session.remove --sid a3f1c2

vox export --sid a3f1c2 --format md
vox export --sid a3f1c2 --format srt --output out.srt
vox export --sid a3f1c2 --format json
```

`hear` prints one YAML document. A short transcript is inlined as `text`; a
long one folds to a `preview` plus `$hints` naming the `vox export` command.

Failures print one YAML document to stderr with `error: DOMAIN_ERROR` and a
closed-set `code`.

### How it runs

Every transcription is a file job: the audio is uploaded to Model Studio's
free 48-hour temporary store, an async task is submitted, and vox polls it
to completion — roughly a minute per 45 minutes of audio.

| `model`         | Model                                | Hotword lists | Super hotwords  | Diarization |
| --------------- | ------------------------------------ | ------------- | --------------- | ----------- |
| `fun` (default) | `fun-asr`                            | yes           | no              | yes         |
| `qwen`          | `qwen-audio-3.0-asr-flash-filetrans` | yes           | yes (weight 50) | yes         |

Both cap at 12 hours / 2GB.

## Vocabularies

A vocabulary is a YAML file. The CLI never edits it — it only reconciles it
with the server-side hotword list.

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
vox vocab.sync --name meeting
vox vocab.prune --dryRun
```

The account cap is 10 lists shared across all models.

## Text to speech

```bash
vox say --text '你好世界' --voice Cherry --speed 1.2
vox say --text 'Save this' --output out.wav
vox voice.list
vox voice.record --file sample.wav --name myvoice
vox cache.status
```

| Voice   | Gender | Language      |
| ------- | ------ | ------------- |
| Cherry  | Female | zh/en         |
| Ethan   | Male   | zh/en         |
| Chelsie | Female | zh/en         |
| Serena  | Female | zh/en         |
| Dylan   | Male   | zh (Beijing)  |
| Jada    | Female | zh (Shanghai) |
| Sunny   | Female | zh (Sichuan)  |

## Storage

```
~/.vox/
  config.json          credentials (0600)
  runs/<sid>/          meta.yaml · audio.<ext> · run.json
  vocabulary/          <name>.yaml + .index.json
  cache/               TTS audio
```

## Feedback

Issues go to [celados/vox](https://github.com/celados/vox/issues) — include the
command, the run id, and the YAML error document.

## License

MIT
