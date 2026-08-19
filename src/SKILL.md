# vox

Offline file transcription and TTS through Alibaba Model Studio. `vox hear`
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
is `{ service: dashscope }` — no token material.

```bash
vox auth.login                  # TTY: prompts for the key
vox auth.login --token sk-...   # non-TTY
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

Vendors: `fun` (default, Fun-ASR) or `qwen`. Both cap at **12 hours / 2 GB**.
Expect roughly a minute per 45 minutes of audio.

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
`invalid_usage` · `io_error` · `api_error`

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
