---
type: Guide
title: vox vocabularies
description: Hotword YAML format and account quota for vox vocab.* and hear --vocab.
---

# Vocabularies

```yaml
# ~/.vox/vocabulary/meeting.yaml
lang: zh
default_weight: 4
words:
  百炼: 5
  Fun-ASR: 5
  赛德克巴莱:
```

Selector: `vox @schema .vocab`.

- Account cap: 10 lists shared across models. One vocabulary used with both models consumes two slots.
- Non-ASCII words are at most 15 characters. Weight is 1–5; 50 is a super hotword that only `qwen` honours.
- MiMo does not support vocabularies.
- `vocab.prune --dryRun` first: it deletes server-side lists no local YAML claims.
