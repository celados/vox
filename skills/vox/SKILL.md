---
name: vox
description: Voice I/O — speak text aloud with TTS voice cloning, or transcribe speech to text with ASR.
---

# vox

Voice I/O through the terminal. TTS with ~500ms latency, ASR with ~1s latency. Powered by Qwen3-TTS and Model Studio ASR (Qwen-Audio-3.0 / Fun-ASR) via the DashScope API. Supports system voices and user-cloned voices.

## When to Use

- User asks you to **read something aloud**, narrate, or speak
- User says "say this", "read this to me", "speak", "announce"
- User wants to **preview how text sounds** in a specific voice
- User wants to **test their cloned voice** with new text
- User wants to **transcribe audio** to text
- User says "listen to this", "what does this say", "transcribe this"

## Commands

### Speak text (TTS)

```bash
# Speak with the user's last-used voice (most common)
vox say "Your text here"

# Speak with a specific system voice
vox say "Hello world" --voice Cherry

# Specify language for better pronunciation
vox say "你好世界" --lang Chinese
vox say "こんにちは" --lang Japanese

# Expressive speech with style instructions
vox say "Welcome to our show!" --instruct "warm and enthusiastic, moderate pace"

# Adjust speech rate
vox say "Slow and clear" --speed 0.8

# Save audio to file
vox say "Save this" --output ~/Desktop/output.wav
```

### Transcribe speech (ASR)

```bash
# Record from microphone (5 seconds default)
vox hear

# Record longer
vox hear -d 10

# Transcribe an existing audio file (format inferred from the extension)
vox hear -f ~/recording.wav

# Provide context for better recognition of domain terms
vox hear -f ~/recording.wav -c "Qwen, DashScope, OnType"

# Force proper nouns with instant hotwords (weight 1-5, or 50 for a super hotword)
vox hear -f ~/recording.wav --hotword DashScope=5 --hotword OnType=5

# Pick the model — Fun-ASR is the alternative engine, no hotword support
vox hear -f ~/recording.wav -m fun-asr-flash-2026-06-15

# Word-level timestamps (subtitles, alignment)
vox hear -f ~/recording.wav --json
```

Models: `qwen-audio-3.0-asr-flash` (default, hotwords + up to 4 language hints)
and `fun-asr-flash-2026-06-15` (context only, first language hint only). Both cap
at 5 minutes / 10MB per request.

### Manage voices

```bash
# List all available voices (system + cloned)
vox voice list

# Enroll a new cloned voice from an audio file
vox voice record --file ~/my-recording.wav --name myvoice

# Record from microphone (language auto-detected from system)
vox voice record --name myvoice

# Specify language for sample text
vox voice record --name myvoice --lang ja

# Delete a cloned voice
vox voice delete <voice-id>
```

### Auth

```bash
# Check if authenticated
vox auth status

# Login (only needed once; prompts for the key, validates before saving)
vox auth login dashscope

# Clear stored credentials
vox auth logout
```

## Behavior

- **Auto voice**: `vox say` without `--voice` uses the last voice. No need to pass `--voice` every time.
- **Auto language**: `vox voice record` without `--lang` detects language from macOS system locale.
- **Caching**: Same text + voice combination plays instantly from cache on repeat.
- **Streaming**: Audio streams to speaker as it generates — no wait for full download.
- **Pipeable ASR**: `vox hear` outputs text to stdout, can be piped to other commands.
- **Language auto-detect**: Usually correct, but pass `--lang` for mixed-language or ambiguous text.

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

## Tips

- For long text, just pass it all — the API handles streaming well.
- If the user has a cloned voice set up, prefer using it (no `--voice` flag needed).
- Check `vox auth status` first if you get auth errors.
- Use `--output` when the user wants to keep the audio file.
- `vox hear` output goes to stdout — stderr has metadata. Pipe-friendly.
- For voice cloning, 10-20 seconds of clear audio works best.
