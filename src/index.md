---
type: Guide
title: vox usage
description: Agent-facing contract for transcription, speaker identity, and speech synthesis.
---

# vox

File transcription and TTS through Alibaba Model Studio, with local MLX speaker identification. `vox hear` uploads a file, transcribes it, and stores a **run**; markdown, subtitles and plain text are exports of that run. No realtime ASR, no microphone transcription. Preflight: `vox status`.

Read the section for your task, then only that slice: `vox @schema .hear`, or several at once with `vox @schema '.{hear,export}'`. Deeper material: `vox @skill references/<file>`.

## Everywhere

- Provider keys need no setup. A command's optional `apiKey` falls back to the env var (`DASHSCOPE_API_KEY`, `MIMO_API_KEY`), then the vault. To use another key pass a reference (`{ ref: 'env:MY_KEY' }`), never the literal: literals land in shell history and transcripts. A key nobody can find is for a person to fix; don't hunt for it.
- Failures exit 1 with `error: DOMAIN_ERROR` and a `code`. Branch on `code`, never on the exit status or detail prose. `session_not_found` → `vox session.list`.
- Stored runs, `export`, `session.*`, `speaker.*` and cached `say` read no key. `VOX_HOME` relocates `~/.vox` for isolated tests.

## Transcribing — `.{hear,session}`

- Pass `--lang` for known-language audio: the cheapest quality win. Pass the whole file; splitting to get under a limit truncates words at every cut. Expect about a minute per 45 minutes of audio.
- `fun` (default) and `qwen` cap at 12 hours / 2 GB. `mimo` takes only MP3/WAV up to 10 MB and returns plain text with no timestamps, speaker labels or vocabularies. Use it for short A/B samples.
- `hear` writes `~/.vox/runs/<sid>/transcript.md` and returns a YAML envelope. A long transcript folds to `preview` plus a `$hints` export command; run that rather than re-reading the envelope. `session.list` is the same records without text.
- The run id hashes audio plus arguments, so repeating a call is a cache hit; `refresh: true` re-recognizes.

## Exporting — `.export`

`export` renders a stored run. Without `--output` stdout is the document itself, not a YAML wrapper. Use `srt`/`vtt`/`txt`/`json` for subtitles and plain text; the envelope is not a subtitle file. Exports use the latest saved speaker-identity snapshot.

## Speaker identity — `.speaker` (and `hear --identify`)

Local, Apple Silicon, needs `uv` and `ffmpeg`. Voiceprints are personal voice data: treat them accordingly. Enrollment rules, matching semantics and storage: `vox @skill references/speaker.md`.

## Vocabularies — `.vocab`

A vocabulary is a YAML file you write at `~/.vox/vocabulary/<name>.yaml`; there is no create or edit command. `hear --vocab <name>` syncs on demand, and editing the file changes the run id. It only biases domain terms; it cannot rescue bad audio. File format and quota: `vox @skill references/vocab.md`.

## Speaking — `.{say,voice}`

- Multi-line text goes as the whole input in YAML (`vox say - <<'EOF'` with `text: |`), not inside a JSON object. A script already on disk is `text: { ref: 'file:<path>' }`; don't paste it back.
- System voices: Cherry, Ethan, Chelsie, Serena (zh/en), Dylan (Beijing), Jada (Shanghai), Sunny (Sichuan). `--output` writes a WAV and does not play.
- `voice.*` is TTS voice cloning; `speaker.*` is recognition identity. They are unrelated.

## Feedback

File issues at [celados/vox](https://github.com/celados/vox/issues) with the command, the run id and the YAML error document.
