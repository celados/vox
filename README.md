# vox

Voice clone TTS, ASR, and Slack listener — powered by [Qwen3-TTS](https://github.com/QwenLM/Qwen3-TTS) and [Qwen3-ASR](https://github.com/QwenLM/Qwen3-ASR) via DashScope API.

Record your voice once, speak in any language. Transcribe speech to text. Listen to Slack channels aloud.

## Install

```bash
go install github.com/celados/vox@latest
```

Requires `ffmpeg` for audio cache compression (`brew install ffmpeg`).

## Quick Start

```bash
# Authenticate with DashScope
vox auth login dashscope --token <your-api-key>

# Speak with a system voice
vox say "Hello world" --voice Cherry

# Clone your voice
vox voice record --file ~/my-voice.wav --name myvoice

# Speak with your cloned voice
vox say "你好世界，这是我的声音。"

# Transcribe speech to text
vox hear -f recording.wav

# Listen to Slack messages aloud
vox auth login slack --bot-token xoxb-... --app-token xapp-...
vox listen -c general
```

## Commands

```
vox auth login dashscope --token <key>     Save DashScope API key
vox auth login slack                       Save Slack tokens
  --bot-token    Slack Bot Token (xoxb-...)
  --app-token    Slack App-Level Token (xapp-...)
vox auth status                            Show all configured services

vox say <text> [flags]                     Speak text with TTS
  -v, --voice      Voice ID or system voice name
  -l, --lang       Language hint (auto, Chinese, English, Japanese, ...)
  -i, --instruct   Voice style instruction (e.g. 'warm and expressive')
  -s, --speed      Speech rate (0.5-2.0, default: 1.0)
  -o, --output     Save audio to WAV file
  --no-cache       Skip audio cache

vox hear [flags]                           Transcribe speech to text
  -f, --file       Transcribe existing audio file
  -d, --duration   Recording duration in seconds (default: 5)
  -c, --context    Text context to improve recognition
  --no-cache       Skip transcription cache

vox listen [flags]                         Listen to Slack and speak messages
  -c, --channel    Channel names or IDs (repeatable, default: all)
  -v, --voice      Default voice for TTS
  -s, --speed      Speech rate (default: 1.2)
  --no-chime       Disable notification chime

vox voice list                             List system + cloned voices
vox voice record [flags]                   Record and enroll a voice clone
  -f, --file       Use existing audio file instead of recording
  -n, --name       Name for the cloned voice
  -l, --lang       Language for sample text (auto-detected if omitted)
  -d, --duration   Recording duration in seconds (default: 15)
vox voice delete <voice-id>                Delete a cloned voice

vox cache                                  Show cache size and file count
vox cache clear                            Delete all cached audio
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

## Slack Listen Mode

`vox listen` connects to Slack via Socket Mode and speaks incoming messages aloud.

### Setup

1. Go to [api.slack.com/apps](https://api.slack.com/apps) and select (or create) your Slack app
2. **Socket Mode** → Enable Socket Mode → Generate an App-Level Token with `connections:write` scope → copy the `xapp-...` token
3. **Event Subscriptions** → Enable Events → Subscribe to bot events: `message.channels` (public) and/or `message.groups` (private)
4. **OAuth & Permissions** → Bot Token Scopes: `channels:history`, `channels:read`, `groups:history`, `groups:read`, `users:read`
5. Install the app to your workspace → copy the `xoxb-...` Bot Token

```bash
vox auth login slack --bot-token xoxb-... --app-token xapp-...
vox listen -c general -c random
```

### Voice Mapping

Map Slack users to specific voices in `~/.vox/config.json`:

```json
{
  "listen": {
    "voice_map": {
      "alice": "Cherry",
      "bob": "Ethan",
      "dio": "qwen-tts-vc-dio-voice-xxx"
    }
  }
}
```

When a user has a mapped voice, vox skips the chime and "From X in Y" announcement — the voice itself identifies the speaker.

Keys can be display names (case-insensitive) or Slack user IDs (`U12345678`).

## How It Works

- **TTS**: WebSocket streaming via DashScope Realtime API → direct audio playback (~500ms to first audio)
- **ASR**: Qwen3-ASR-Flash via DashScope multimodal API → synchronous transcription (~1.5s latency)
- **Voice Clone**: Upload reference audio → DashScope enrolls a voice profile → use the voice ID for TTS
- **Instruct Mode**: Pass `--instruct` for expressive speech (system voices only, uses `qwen3-tts-instruct-flash-realtime`)
- **Caching**: TTS audio cached as Opus (~20x smaller than PCM). ASR transcriptions cached as text.
- **State**: Last used voice ID remembered in `~/.vox/state.json`

## API Keys

| Service | Where to get | What you need |
|---------|-------------|---------------|
| DashScope | [阿里云百炼](https://bailian.console.aliyun.com/) | API key (`sk-...`) |
| Slack | [api.slack.com/apps](https://api.slack.com/apps) | Bot Token (`xoxb-...`) + App-Level Token (`xapp-...`) |

All credentials are stored locally in `~/.vox/config.json`.

## License

MIT
