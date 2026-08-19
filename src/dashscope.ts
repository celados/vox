import { basename } from "node:path";

export const HTTP_ENDPOINT = "https://dashscope.aliyuncs.com/api/v1";
export const MODEL_FUN_ASR = "fun-asr";
export const MODEL_QWEN_AUDIO_FILE = "qwen-audio-3.0-asr-flash-filetrans";
export const MODEL_FLASH_REALTIME = "qwen3-tts-flash-realtime";
export const MODEL_INSTRUCT_REALTIME = "qwen3-tts-instruct-flash-realtime";
export const MODEL_VC_REALTIME = "qwen3-tts-vc-realtime-2026-01-15";
export const MODEL_ENROLLMENT = "qwen-voice-enrollment";
export const BIASING_MODEL = "speech-biasing";

export const MAX_FILE_SECONDS = 12 * 60 * 60;
export const MAX_FILE_BYTES = 2 * 1024 * 1024 * 1024;
export const DIARIZATION_MAX_SECONDS = 2 * 60 * 60;
export const VOCABULARY_QUOTA = 10;

const TRANSCRIPTION_PATH = "/services/audio/asr/transcription";
const TASKS_PATH = "/tasks/";
const CUSTOMIZATION_PATH = "/services/audio/asr/customization";
const ENROLLMENT_PATH = "/services/audio/tts/customization";
const UPLOADS_PATH = "/uploads";
const POLL_INTERVAL_MS = 5_000;

export const VENDORS = {
  fun: MODEL_FUN_ASR,
  qwen: MODEL_QWEN_AUDIO_FILE,
} as const;

export type Vendor = keyof typeof VENDORS;

export function resolveModel(vendor: string): string | undefined {
  if (vendor in VENDORS) return VENDORS[vendor as Vendor];
  return undefined;
}

export function superHotwordsSupported(model: string): boolean {
  return model === MODEL_QWEN_AUDIO_FILE;
}

export type AsrWord = {
  text: string;
  begin_time: number;
  end_time: number;
  punctuation: string;
  confidence?: number;
};

export type Sentence = {
  begin_time: number;
  end_time: number;
  text: string;
  speaker?: string;
  words?: AsrWord[];
};

export type AsrResult = {
  text: string;
  sentences?: Sentence[];
  duration_sec?: number;
};

export function asrWords(result: AsrResult): AsrWord[] {
  const words: AsrWord[] = [];
  for (const sentence of result.sentences ?? []) {
    words.push(...(sentence.words ?? []));
  }
  return words;
}

export type Hotword = {
  text: string;
  weight: number;
  lang?: string;
};

export type VocabularyInfo = {
  id: string;
  status: string;
  target_model?: string;
  created?: string;
  modified?: string;
};

export type FileOptions = {
  model: string;
  vocabularyId?: string;
  languageHints?: string[];
  diarization?: boolean;
};

export type TaskProgress = (status: string, elapsedMs: number) => void;

export type SystemVoice = {
  id: string;
  name: string;
  language: string;
  gender: string;
  model: string;
};

export const SYSTEM_VOICES: SystemVoice[] = [
  {
    id: "Cherry",
    name: "Cherry",
    language: "zh/en",
    gender: "Female",
    model: MODEL_FLASH_REALTIME,
  },
  { id: "Ethan", name: "Ethan", language: "zh/en", gender: "Male", model: MODEL_FLASH_REALTIME },
  {
    id: "Chelsie",
    name: "Chelsie",
    language: "zh/en",
    gender: "Female",
    model: MODEL_FLASH_REALTIME,
  },
  {
    id: "Serena",
    name: "Serena",
    language: "zh/en",
    gender: "Female",
    model: MODEL_FLASH_REALTIME,
  },
  {
    id: "Dylan",
    name: "Dylan",
    language: "zh (Beijing)",
    gender: "Male",
    model: MODEL_FLASH_REALTIME,
  },
  {
    id: "Jada",
    name: "Jada",
    language: "zh (Shanghai)",
    gender: "Female",
    model: MODEL_FLASH_REALTIME,
  },
  {
    id: "Sunny",
    name: "Sunny",
    language: "zh (Sichuan)",
    gender: "Female",
    model: MODEL_FLASH_REALTIME,
  },
];

export function isSystemVoice(voiceId: string): boolean {
  return SYSTEM_VOICES.some((voice) => voice.id === voiceId);
}

export function modelForVoice(voiceId: string): string {
  return isSystemVoice(voiceId) ? MODEL_FLASH_REALTIME : MODEL_VC_REALTIME;
}

export type ClonedVoice = {
  voice: string;
  language?: string;
  target_model?: string;
};

export class DashScopeClient {
  readonly apiKey: string;

  constructor(apiKey: string) {
    this.apiKey = apiKey;
  }

  async validate(): Promise<void> {
    await this.listVoices(0, 1);
  }

  async transcribeFile(
    filename: string,
    data: Uint8Array,
    opt: FileOptions,
    progress?: TaskProgress,
  ): Promise<AsrResult> {
    const model = opt.model || MODEL_FUN_ASR;
    const fileUrl = await this.uploadTemporary(model, filename, data);
    const params: Record<string, unknown> = {};
    if (opt.vocabularyId) params.vocabulary_id = opt.vocabularyId;
    if (opt.languageHints && opt.languageHints.length > 0)
      params.language_hints = opt.languageHints;
    if (opt.diarization) params.diarization_enabled = true;

    const resp = await this.post(
      TRANSCRIPTION_PATH,
      {
        model,
        input: { file_urls: [fileUrl] },
        parameters: params,
      },
      {
        "X-DashScope-Async": "enable",
        "X-DashScope-OssResourceResolve": "enable",
      },
    );
    const output = asRecord(resp.output);
    const taskId = asString(output?.task_id);
    if (!taskId) throw new Error("no task_id in response");
    const transcriptUrl = await this.awaitTask(taskId, progress);
    return await this.fetchTranscript(transcriptUrl);
  }

  async uploadTemporary(model: string, filename: string, data: Uint8Array): Promise<string> {
    const policy = await this.uploadPolicy(model);
    const key = `${policy.upload_dir}/${basename(filename)}`;
    const form = new FormData();
    // OSS ignores fields written after the file part.
    form.append("key", key);
    form.append("policy", policy.policy);
    form.append("OSSAccessKeyId", policy.oss_access_key_id);
    form.append("signature", policy.signature);
    form.append("x-oss-object-acl", policy.x_oss_object_acl);
    form.append("x-oss-forbid-overwrite", policy.x_oss_forbid_overwrite);
    form.append("success_action_status", "200");
    const copy = new Uint8Array(data.byteLength);
    copy.set(data);
    form.append("file", new Blob([copy]), basename(filename));

    const resp = await fetch(policy.upload_host, { method: "POST", body: form });
    if (!resp.ok && resp.status !== 204) {
      const detail = await resp.text();
      throw new Error(`upload failed: HTTP ${resp.status}: ${detail}`);
    }
    return `oss://${key}`;
  }

  async createVocabulary(targetModel: string, prefix: string, words: Hotword[]): Promise<string> {
    const resp = await this.post(CUSTOMIZATION_PATH, {
      model: BIASING_MODEL,
      input: {
        action: "create_vocabulary",
        target_model: targetModel,
        prefix,
        vocabulary: words,
      },
    });
    const id = asString(asRecord(resp.output)?.vocabulary_id);
    if (!id) throw new Error("no vocabulary_id in response");
    return id;
  }

  async updateVocabulary(vocabularyId: string, words: Hotword[]): Promise<void> {
    await this.post(CUSTOMIZATION_PATH, {
      model: BIASING_MODEL,
      input: {
        action: "update_vocabulary",
        vocabulary_id: vocabularyId,
        vocabulary: words,
      },
    });
  }

  async queryVocabulary(vocabularyId: string): Promise<{ info: VocabularyInfo; words: Hotword[] }> {
    const resp = await this.post(CUSTOMIZATION_PATH, {
      model: BIASING_MODEL,
      input: { action: "query_vocabulary", vocabulary_id: vocabularyId },
    });
    const output = asRecord(resp.output);
    if (!output) throw new Error("unexpected response: missing output");
    const info: VocabularyInfo = {
      id: vocabularyId,
      status: asString(output.status) ?? "",
      target_model: asString(output.target_model),
      created: asString(output.gmt_create),
      modified: asString(output.gmt_modified),
    };
    const words: Hotword[] = [];
    if (Array.isArray(output.vocabulary)) {
      for (const item of output.vocabulary) {
        const row = asRecord(item);
        if (!row) continue;
        const text = asString(row.text) ?? "";
        words.push({
          text,
          weight: jsonInt(row.weight),
          lang: asString(row.lang),
        });
      }
    }
    return { info, words };
  }

  async listVocabularies(): Promise<VocabularyInfo[]> {
    const resp = await this.post(CUSTOMIZATION_PATH, {
      model: BIASING_MODEL,
      input: { action: "list_vocabulary", page_index: 0, page_size: VOCABULARY_QUOTA },
    });
    const output = asRecord(resp.output);
    const raw = output?.vocabulary_list;
    const list: VocabularyInfo[] = [];
    if (!Array.isArray(raw)) return list;
    for (const item of raw) {
      const row = asRecord(item);
      if (!row) continue;
      list.push({
        id: asString(row.vocabulary_id) ?? "",
        status: asString(row.status) ?? "",
        created: asString(row.gmt_create),
        modified: asString(row.gmt_modified),
      });
    }
    return list;
  }

  async deleteVocabulary(vocabularyId: string): Promise<void> {
    await this.post(CUSTOMIZATION_PATH, {
      model: BIASING_MODEL,
      input: { action: "delete_vocabulary", vocabulary_id: vocabularyId },
    });
  }

  async awaitVocabulary(vocabularyId: string, timeoutMs: number): Promise<void> {
    const deadline = Date.now() + timeoutMs;
    for (;;) {
      const { info } = await this.queryVocabulary(vocabularyId);
      if (info.status === "OK") return;
      if (Date.now() > deadline) {
        throw new Error(`vocabulary ${vocabularyId} still ${info.status} after ${timeoutMs}ms`);
      }
      await sleep(500);
    }
  }

  async enrollVoice(name: string, audioBase64: string): Promise<string> {
    const resp = await this.post(ENROLLMENT_PATH, {
      model: MODEL_ENROLLMENT,
      input: {
        action: "create",
        target_model: MODEL_VC_REALTIME,
        preferred_name: name,
        audio: { data: `data:audio/wav;base64,${audioBase64}` },
      },
    });
    const voiceId = asString(asRecord(resp.output)?.voice);
    if (!voiceId) throw new Error(`no voice in response: ${JSON.stringify(resp.output)}`);
    return voiceId;
  }

  async listVoices(page: number, pageSize: number): Promise<ClonedVoice[]> {
    const resp = await this.post(ENROLLMENT_PATH, {
      model: MODEL_ENROLLMENT,
      input: { action: "list", page_size: pageSize, page_index: page },
    });
    const output = asRecord(resp.output);
    const raw = output?.voice_list;
    if (!Array.isArray(raw)) return [];
    const voices: ClonedVoice[] = [];
    for (const item of raw) {
      const row = asRecord(item);
      if (!row) continue;
      const voice = asString(row.voice);
      if (!voice) continue;
      voices.push({
        voice,
        language: asString(row.language),
        target_model: asString(row.target_model),
      });
    }
    return voices;
  }

  async deleteVoice(voiceId: string): Promise<void> {
    await this.post(ENROLLMENT_PATH, {
      model: MODEL_ENROLLMENT,
      input: { action: "delete", voice: voiceId },
    });
  }

  private async uploadPolicy(model: string): Promise<{
    upload_host: string;
    upload_dir: string;
    oss_access_key_id: string;
    signature: string;
    policy: string;
    x_oss_object_acl: string;
    x_oss_forbid_overwrite: string;
  }> {
    const url = new URL(HTTP_ENDPOINT + UPLOADS_PATH);
    url.searchParams.set("action", "getPolicy");
    url.searchParams.set("model", model);
    const resp = await fetch(url, { headers: { Authorization: `Bearer ${this.apiKey}` } });
    const raw = await resp.text();
    if (!resp.ok) throw new Error(`HTTP ${resp.status}: ${raw}`);
    const envelope = JSON.parse(raw) as { data?: Record<string, unknown> };
    const data = envelope.data ?? {};
    const host = asString(data.upload_host);
    if (!host) throw new Error(`upload policy has no host: ${raw}`);
    return {
      upload_host: host,
      upload_dir: asString(data.upload_dir) ?? "",
      oss_access_key_id: asString(data.oss_access_key_id) ?? "",
      signature: asString(data.signature) ?? "",
      policy: asString(data.policy) ?? "",
      x_oss_object_acl: asString(data.x_oss_object_acl) ?? "",
      x_oss_forbid_overwrite: asString(data.x_oss_forbid_overwrite) ?? "",
    };
  }

  private async awaitTask(taskId: string, progress?: TaskProgress): Promise<string> {
    const start = Date.now();
    for (;;) {
      const resp = await fetch(HTTP_ENDPOINT + TASKS_PATH + taskId, {
        headers: { Authorization: `Bearer ${this.apiKey}` },
      });
      const raw = await resp.text();
      const task = JSON.parse(raw) as {
        output?: {
          task_status?: string;
          message?: string;
          results?: Array<{
            transcription_url?: string;
            subtask_status?: string;
            message?: string;
          }>;
        };
      };
      const status = task.output?.task_status ?? "";
      progress?.(status, Date.now() - start);
      if (status === "SUCCEEDED") {
        const result = task.output?.results?.[0];
        if (!result) throw new Error("task succeeded with no results");
        if (!result.transcription_url) {
          throw new Error(`task succeeded without a transcript: ${result.message ?? ""}`);
        }
        return result.transcription_url;
      }
      if (status === "FAILED" || status === "CANCELED") {
        const detail = task.output?.message || task.output?.results?.[0]?.message || "";
        throw new Error(`task ${status}: ${detail}`);
      }
      await sleep(POLL_INTERVAL_MS);
    }
  }

  private async fetchTranscript(url: string): Promise<AsrResult> {
    const resp = await fetch(url);
    const raw = await resp.text();
    if (!resp.ok) throw new Error(`transcript download: HTTP ${resp.status}`);
    const doc = JSON.parse(raw) as {
      properties?: { original_duration_in_milliseconds?: number };
      transcripts?: Array<{
        text?: string;
        sentences?: Array<{
          begin_time?: number;
          end_time?: number;
          text?: string;
          speaker_id?: unknown;
          words?: Array<{
            begin_time?: number;
            end_time?: number;
            text?: string;
            punctuation?: string;
            confidence?: number;
          }>;
        }>;
      }>;
    };
    const channel = doc.transcripts?.[0];
    if (!channel) throw new Error("transcript document has no channels");
    const result: AsrResult = {
      text: channel.text ?? "",
      duration_sec: Math.floor((doc.properties?.original_duration_in_milliseconds ?? 0) / 1000),
      sentences: [],
    };
    for (const sentence of channel.sentences ?? []) {
      const words: AsrWord[] = [];
      for (const word of sentence.words ?? []) {
        words.push({
          text: word.text ?? "",
          begin_time: word.begin_time ?? 0,
          end_time: word.end_time ?? 0,
          punctuation: word.punctuation ?? "",
          confidence: word.confidence,
        });
      }
      result.sentences!.push({
        begin_time: sentence.begin_time ?? 0,
        end_time: sentence.end_time ?? 0,
        text: sentence.text ?? "",
        speaker: speakerLabel(sentence.speaker_id),
        words,
      });
    }
    return result;
  }

  private async post(
    path: string,
    body: unknown,
    headers: Record<string, string> = {},
  ): Promise<Record<string, unknown>> {
    const resp = await fetch(HTTP_ENDPOINT + path, {
      method: "POST",
      headers: {
        Authorization: `Bearer ${this.apiKey}`,
        "Content-Type": "application/json",
        ...headers,
      },
      body: JSON.stringify(body),
    });
    const raw = await resp.text();
    if (!resp.ok) throw new Error(`HTTP ${resp.status}: ${raw}`);
    return JSON.parse(raw) as Record<string, unknown>;
  }
}

function speakerLabel(value: unknown): string | undefined {
  if (typeof value === "string" && value) return value;
  if (typeof value === "number") return String(Math.trunc(value));
  return undefined;
}

function asRecord(value: unknown): Record<string, unknown> | undefined {
  if (value !== null && typeof value === "object" && !Array.isArray(value)) {
    return value as Record<string, unknown>;
  }
  return undefined;
}

function asString(value: unknown): string | undefined {
  return typeof value === "string" && value ? value : undefined;
}

function jsonInt(value: unknown): number {
  return typeof value === "number" ? Math.trunc(value) : 0;
}

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}
