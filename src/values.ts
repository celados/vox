import { defineValueSource, env } from "@celados/argc/values";
import { latch } from "@celados/latch-source";
import * as v from "valibot";

/**
 * Provider keys, resolved on first await: env var first (CI, one-off overrides),
 * then the celados-agent vault item. Commands that never call a provider never
 * touch either source.
 */
export const secret = defineValueSource({
  DASHSCOPE_API_KEY: {
    schema: v.pipe(v.string(), v.trim(), v.minLength(1)),
    description: "Alibaba Model Studio (DashScope) API key — ASR fun/qwen, TTS, voices, vocab",
    get: [
      env("DASHSCOPE_API_KEY"),
      latch("7c38804e-df94-444c-8897-efcde8a77339/凭据", { item: "bailian-api-key" }),
    ],
  },
  MIMO_API_KEY: {
    schema: v.pipe(v.string(), v.trim(), v.minLength(1)),
    description: "Xiaomi MiMo API key — ASR model mimo",
    get: [
      env("MIMO_API_KEY"),
      latch("52f369e9-e50d-4e49-89e7-a8901c944f41/api key", { item: "xiaomi-mimo / api-key" }),
    ],
  },
});

/** argc's DOMAIN_ERROR for a value no source could supply. */
export function isValueUnavailable(
  error: unknown,
): error is Error & { envelope: { tried: unknown } } {
  return (
    error instanceof Error &&
    "envelope" in error &&
    (error as { envelope?: { code?: unknown } }).envelope?.code === "value_unavailable"
  );
}
