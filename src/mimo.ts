import type { AsrResult } from "./dashscope.ts";

export const MIMO_ENDPOINT = "https://api.xiaomimimo.com/v1";
export const MODEL_MIMO_ASR = "mimo-v2.5-asr";
export const MIMO_MAX_BASE64_BYTES = 10_000_000;

type Fetcher = typeof fetch;

export type MimoLanguage = "auto" | "zh" | "en";

export function mimoLanguage(langs: string[] | undefined): MimoLanguage {
  if (!langs || langs.length === 0) return "auto";
  if (langs.length === 1 && (langs[0] === "zh" || langs[0] === "en")) return langs[0];
  throw new Error('MiMo accepts one language: "zh" or "en"');
}

export function mimoAudioData(data: Uint8Array, format: string): string {
  const mime = format === "mp3" ? "audio/mpeg" : format === "wav" ? "audio/wav" : "";
  if (!mime) throw new Error("MiMo accepts only MP3 or WAV audio");
  const encoded = Buffer.from(data).toString("base64");
  if (encoded.length > MIMO_MAX_BASE64_BYTES) {
    throw new Error(
      `MiMo audio exceeds the 10 MB Base64 limit (${encoded.length} bytes after encoding)`,
    );
  }
  return `data:${mime};base64,${encoded}`;
}

export class MimoClient {
  readonly apiKey: string;
  readonly fetcher: Fetcher;

  constructor(apiKey: string, fetcher: Fetcher = fetch) {
    this.apiKey = apiKey;
    this.fetcher = fetcher;
  }

  async validate(): Promise<void> {
    const response = await this.fetcher(`${MIMO_ENDPOINT}/models`, {
      headers: { "api-key": this.apiKey },
    });
    if (!response.ok) throw new Error(await responseDetail(response));
  }

  async transcribe(data: Uint8Array, format: string, langs?: string[]): Promise<AsrResult> {
    const response = await this.fetcher(`${MIMO_ENDPOINT}/chat/completions`, {
      method: "POST",
      headers: {
        "api-key": this.apiKey,
        "Content-Type": "application/json",
      },
      body: JSON.stringify({
        model: MODEL_MIMO_ASR,
        messages: [
          {
            role: "user",
            content: [
              {
                type: "input_audio",
                input_audio: { data: mimoAudioData(data, format) },
              },
            ],
          },
        ],
        asr_options: { language: mimoLanguage(langs) },
      }),
    });
    if (!response.ok) throw new Error(await responseDetail(response));
    const payload = (await response.json()) as Record<string, unknown>;
    const text = completionText(payload);
    if (!text) throw new Error("MiMo response has no transcript text");
    const usage = asRecord(payload.usage);
    const duration = Number(usage?.seconds ?? 0);
    return {
      text,
      duration_sec: Number.isFinite(duration) && duration > 0 ? duration : undefined,
    };
  }
}

export function completionText(payload: Record<string, unknown>): string {
  const choices = payload.choices;
  if (!Array.isArray(choices)) return "";
  const first = asRecord(choices[0]);
  const message = asRecord(first?.message);
  return typeof message?.content === "string" ? message.content.trim() : "";
}

function asRecord(value: unknown): Record<string, unknown> | undefined {
  return typeof value === "object" && value !== null
    ? (value as Record<string, unknown>)
    : undefined;
}

async function responseDetail(response: Response): Promise<string> {
  const text = (await response.text()).trim();
  return `MiMo API ${response.status}: ${text.slice(0, 500) || response.statusText}`;
}
