# Backlog

## ASR 迁移遗留（2026-08-01）

- **未迁移到业务空间专属域名**：仍用 `dashscope.aliyuncs.com`，官方推荐
  `{WorkspaceId}.cn-beijing.maas.aliyuncs.com`（性能/稳定性更好）。需要先拿到
  Workspace ID 并加进 config。老域名官方声明仍可用。
- **只做了非实时 ASR**：realtime（WebSocket，`fun-asr-realtime` /
  `qwen3-asr-flash-realtime`）和长音频异步 filetrans（12 小时上限）都没接。
  当前 5 分钟 / 10MB 上限对会议录音不够。
- **`qwen3-asr-flash` 老接口已删**：走的是 OpenAI 兼容 schema，与新接口不共用
  代码路径。如需情感识别（新模型不支持）要把它加回来。
- **lipgloss 仍在 v1.1.0**：v2 是独立 module path，`go get -u` 不会带过去。
- **Slack app 凭据已从本地 config 清除，但 Slack 侧的 app/token 未吊销**。
