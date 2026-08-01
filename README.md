# vox

Voice clone TTS and ASR — powered by [Qwen3-TTS](https://github.com/QwenLM/Qwen3-TTS) and Alibaba Model Studio ASR via the DashScope API.

Record your voice once, speak in any language. Transcribe speech to text.

## Install

```bash
go install github.com/celados/vox@latest
```

Requires `ffmpeg` for audio cache compression (`brew install ffmpeg`).

## Quick Start

```bash
# Authenticate with DashScope (prompts for the key, validates before saving)
vox auth login dashscope

# Speak with a system voice
vox say "Hello world" --voice Cherry

# Clone your voice
vox voice record --file ~/my-voice.wav --name myvoice

# Speak with your cloned voice
vox say "你好世界，这是我的声音。"

# Transcribe speech to text
vox hear -f recording.wav
```

## Commands

```
vox auth login dashscope [--token <key>]   Save DashScope API key (prompted when --token is omitted)
vox auth logout                            Clear stored credentials
vox auth status                            Show configured services

vox say <text> [flags]                     Speak text with TTS
  -v, --voice      Voice ID or system voice name
  -l, --lang       Language hint (auto, Chinese, English, Japanese, ...)
  -i, --instruct   Voice style instruction (e.g. 'warm and expressive')
  -s, --speed      Speech rate (0.5-2.0, default: 1.0)
  -o, --output     Save audio to WAV file
  --no-cache       Skip audio cache

vox hear [flags]                           Transcribe speech to text
  -f, --file       Transcribe existing audio file (format taken from extension)
  -d, --duration   Recording duration in seconds (default: 5)
  -m, --model      ASR model (default: qwen-audio-3.0-asr-flash)
  -c, --context    Text context to improve recognition (repeatable)
  -l, --lang       Language hint, e.g. zh or en (repeatable; auto-detect when unset)
  --hotword        Instant hotword as word=weight (repeatable, qwen model only)
  --json           Emit the full result including word-level timestamps
  --no-cache       Skip transcription cache

vox voice list                             List system + cloned voices
vox voice record [flags]                   Record and enroll a voice clone
  -f, --file       Use existing audio file instead of recording
  -n, --name       Name for the cloned voice
  -l, --lang       Language for sample text (auto-detected if omitted)
  -d, --duration   Recording duration in seconds (default: 15)
vox voice delete <voice-id>                Delete a cloned voice

vox cache status                           Show cache size and file count
vox cache clear                            Delete all cached audio
```

## ASR Models

Both models share one HTTP endpoint and one request/response schema, so `--model`
is the only thing that changes. Source of truth:
[非实时语音识别 API 参考](https://help.aliyun.com/zh/model-studio/non-real-time-speech-recognition-for-fun-asr-flash)
and the [ASR model list](https://help.aliyun.com/zh/model-studio/asr-model/).

| Model | Max audio | Hotwords | Prompt context | Language hints |
|-------|-----------|----------|----------------|----------------|
| `qwen-audio-3.0-asr-flash` (default) | 5 min / 10MB per request | instant + precompiled | yes | up to 4 |
| `fun-asr-flash-2026-06-15` | 5 min / 10MB per request | no | yes | first one only |

Both cover Mandarin plus major dialects (Cantonese, Wu, Hokkien, Hakka, and
regional accents) and ~30 other languages.

Accuracy on proper nouns comes from hotwords or context:

```bash
# instant hotwords — weight 1-5, or 50 for a "super" hotword (qwen model only)
vox hear -f meeting.wav --hotword 百炼=5 --hotword Fun-ASR=5

# prompt context — works on both models
vox hear -f meeting.wav -m fun-asr-flash-2026-06-15 -c "百炼 Fun-ASR Qwen 语音识别"
```

`--json` returns word-level timestamps:

```json
{
  "text": "百炼语音识别测试。",
  "words": [{ "text": "百炼", "begin_time": 160, "end_time": 520, "punctuation": "" }],
  "duration_sec": 5
}
```

## System Voices

| Voice | Gender | Language |
|-------|--------|----------|
| Cherry | Female | zh/en |
| Ethan | Male | zh/en |
| Chelsie | Female | zh/en |
| Serena | Female | zh/en |
| Dylan | Male | zh (Beijing) |
| Jada | Female | zh (Shanghai) |
| Sunny | Female | zh (Sichuan) |

## Supported Languages

Voice cloning sample texts: zh, en, ja, ko, de, fr, es, pt, it, ru, pl, sv, da, fi, no, cs, is.

Language is auto-detected from macOS system locale when `--lang` is omitted.

## How It Works

- **TTS**: WebSocket streaming via DashScope Realtime API → direct audio playback (~500ms to first audio)
- **ASR**: non-streaming multimodal-generation endpoint → transcript + word timestamps (~1s latency)
- **Voice Clone**: Upload reference audio → DashScope enrolls a voice profile → use the voice ID for TTS
- **Instruct Mode**: Pass `--instruct` for expressive speech (system voices only, uses `qwen3-tts-instruct-flash-realtime`)
- **Caching**: TTS audio cached as Opus (~20x smaller than PCM). ASR results cached as JSON, keyed by audio *and* recognition options.
- **State**: Last used voice ID remembered in `~/.vox/state.json`

## API Keys

Get a DashScope key from [阿里云百炼](https://bailian.console.aliyun.com/). It is
stored locally in `~/.vox/config.json` (mode 0600) and validated against the API
before it is written.

## License

MIT
