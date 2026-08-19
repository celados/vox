# Backlog

Accepted trade-offs with a known tail. Resolved items are deleted rather than
archived — git history is the record.

- **Not on the workspace-specific domain**: still `dashscope.aliyuncs.com`.
  Alibaba recommends `{WorkspaceId}.cn-beijing.maas.aliyuncs.com` for latency and
  stability, which needs a Workspace ID in the config. The legacy host is
  documented as still supported, and every endpoint vox uses works on it.
- **Slack app not revoked**: the tokens were removed from `~/.vox/config.json`,
  but the Slack app itself still exists workspace-side. A decision, not a task.
- **Concurrent `hear` on one file pays twice**: publication is atomic, so a
  stored run is never inconsistent, but two processes that miss simultaneously
  both submit a task. A per-sid lock would close it; not worth the machinery
  until it happens.
- **TTS is not on runs yet**: `say` / `voice` / `cache` predate the
  content-addressed model. `cache` is a run without metadata or an index, and
  should fold into `session` when TTS is redesigned.
- **Mic enrollment needs ffmpeg**: the Go binary linked miniaudio. The argc
  rewrite shells out to `ffmpeg` (avfoundation / pulse) instead of shipping
  cgo. `--file` is the non-TTY path.
