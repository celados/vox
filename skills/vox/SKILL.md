---
name: vox
description: Voice I/O — transcribe audio to text, subtitles or markdown, and speak text aloud with TTS voice cloning.
---

# vox

Offline file transcription and TTS through the terminal, over Alibaba Model
Studio. `vox hear` uploads a file, transcribes it, and stores a **run**;
everything after that — markdown, subtitles, plain text — is an export from that
run, with no re-recognition. There is no realtime mode and no mic capture for
ASR; files up to 12 hours are ordinary input.

## When to Use

- Transcribe audio, or produce **subtitles** (SRT/VTT) or a markdown transcript
- User says "transcribe this", "what does this say", "make subtitles"
- Read something aloud, narrate, preview how text sounds in a voice
- Test a cloned voice with new text

## Speech to text

```bash
vox hear recording.wav                     # → YAML envelope carrying the run id
vox hear lecture.mp3 --lang zh             # pin the language — the biggest quality lever
vox hear meeting.m4a --speakers            # label speakers (under ~2 hours)
vox hear recording.wav --vocab meeting     # boost domain terms (see Vocabularies)
vox hear recording.wav --refresh           # re-recognize and overwrite

vox session ls                             # run index, newest first
vox session ls --file recording.wav        # runs for one source file
vox session rm a3f1c2                      # any unique prefix works

vox export a3f1c2 --format md              # markdown with frontmatter
vox export a3f1c2 --format srt -o out.srt  # subtitles
vox export a3f1c2 --format txt             # plain transcript, pipeable
vox export a3f1c2 --format json            # word-level timestamps
```

Vendors: `-m fun` (default, Fun-ASR) or `-m qwen` (Qwen-Audio). Each vendor
ships many variants — realtime, flash, 8k, dated snapshots — and vox pins one
file-transcription model per vendor, so the alias is all you ever pass.
Both cap at **12 hours / 2GB**. A transcription is an upload plus an async task,
so expect roughly a minute per 45 minutes of audio — long files are normal, not
an edge case. Do **not** split a file to work around length: splitting truncates
words at every cut and loses the service's own sentence boundaries.

## Reading the output

`hear` and the `ls` commands print **one YAML document on stdout**; progress and
warnings go to stderr. There are no `--json` or `--sid` flags.

A short transcript is inlined as `text`. A long one is folded: the envelope
carries `preview` plus a `$vox` hint naming the `vox export` command that reads
the rest. Do not try to widen the envelope — run the export.

Failures are one YAML document on stderr with a closed-set `code`. Branch on the
code, do not parse the message:

`not_authenticated` · `audio_too_large` · `audio_unsupported` ·
`vocab_not_found` · `vocab_quota_exceeded` · `vocab_model_mismatch` ·
`vocab_index_corrupt` · `session_not_found` · `session_ambiguous` ·
`invalid_usage` · `io_error` · `api_error`

## Vocabularies

Hotword lists fix proper nouns the model keeps getting wrong. A vocabulary is a
YAML file you **write directly** — there is no create/edit command. Get the path
from `vox vocab ls`, then use the normal file tools.

```yaml
# ~/.vox/vocabulary/meeting.yaml
lang: zh
default_weight: 4
words:
  百炼: 5
  Fun-ASR: 5
  赛德克巴莱:           # empty → default_weight
```

```bash
vox vocab ls                  # names, paths, sync state, remote quota
vox vocab prune --dry-run     # server-side lists nothing local claims
```

`vox hear --vocab meeting` syncs on demand, so `vox vocab sync` is rarely needed.
Editing the YAML changes the run id, so the next `hear` re-recognizes on its own.

Two limits worth knowing before creating vocabularies: **10 lists per account,
shared across models** (one vocabulary used with both models consumes two), and
words are at most 15 characters when non-ASCII. Weight is 1–5; 50 is a "super
hotword" that only `qwen` honours and is clamped elsewhere.

## Text to speech

```bash
vox say "Your text here"                          # last-used voice
vox say "Hello world" --voice Cherry
vox say "こんにちは" --lang Japanese
vox say "Welcome!" --instruct "warm and enthusiastic"
vox say "Slow and clear" --speed 0.8
vox say "Save this" --output ~/Desktop/out.wav
```

System voices: Cherry, Ethan, Chelsie, Serena (zh/en), Dylan (Beijing), Jada
(Shanghai), Sunny (Sichuan).

```bash
vox voice list
vox voice record --file ~/sample.wav --name myvoice   # enroll a clone
vox voice delete <voice-id>
```

## Auth

```bash
vox auth status
vox auth login dashscope      # prompts for the key, validates before storing
vox auth logout
```

## Tips

- Re-running `vox hear` on the same file is free — it returns the stored run.
- `--lang` is the cheapest quality win on known-language audio; measured, it
  fixed proper nouns that a hotword vocabulary did not.
- `--vocab` is for domain terms the model cannot know; it cannot recover audio,
  only bias what the model hears.
- `vox export --format txt` is the pipeable form; the envelope is not.
- For voice cloning, 10–20 seconds of clean audio works best.
- If a cloned voice exists, `vox say` uses it without `--voice`.
