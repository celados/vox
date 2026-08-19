import { MODEL_FLASH_REALTIME } from "./dashscope.ts";

const WS_ENDPOINT = "wss://dashscope.aliyuncs.com/api-ws/v1/realtime";
export const STREAM_TIMEOUT_MS = 2 * 60 * 1000;

export type TtsOptions = {
  model: string;
  voice: string;
  text: string;
  lang?: string;
  instruct?: string;
  speechRate?: number;
};

export async function streamTts(
  apiKey: string,
  opts: TtsOptions,
  onAudio: (pcm: Uint8Array) => void,
): Promise<void> {
  const model = opts.model || MODEL_FLASH_REALTIME;
  const url = `${WS_ENDPOINT}?model=${encodeURIComponent(model)}`;
  // Bun accepts protocol headers; the DOM lib types do not.
  const ws = new WebSocket(url, { headers: { Authorization: `Bearer ${apiKey}` } } as never);

  await waitOpen(ws);
  await expectMessage(ws, "session.created");

  const speechRate = opts.speechRate && opts.speechRate !== 0 ? opts.speechRate : 1.0;
  const session: Record<string, unknown> = {
    voice: opts.voice,
    response_format: "pcm",
    sample_rate: 24_000,
    mode: "server_commit",
    language_type: opts.lang || "auto",
    volume: 50,
    speech_rate: speechRate,
    pitch_rate: 1.0,
  };
  if (opts.instruct) {
    session.instructions = opts.instruct;
    session.optimize_instructions = true;
  }

  sendJson(ws, { type: "session.update", session });
  sendJson(ws, { type: "input_text_buffer.append", text: opts.text });
  sendJson(ws, { type: "session.finish" });

  let timedOut = false;
  const timer = setTimeout(() => {
    timedOut = true;
    ws.close();
  }, STREAM_TIMEOUT_MS);

  try {
    for (;;) {
      const msg = await readJson(ws);
      switch (msg.type) {
        case "response.audio.delta": {
          if (typeof msg.delta === "string") onAudio(Buffer.from(msg.delta, "base64"));
          break;
        }
        case "response.done":
          break;
        case "session.finished":
          return;
        case "error":
          throw new Error(`server error: ${JSON.stringify(msg)}`);
      }
    }
  } catch (error) {
    if (timedOut) throw new Error(`TTS stream timed out after ${STREAM_TIMEOUT_MS / 1000}s`);
    throw error;
  } finally {
    clearTimeout(timer);
    ws.close();
  }
}

function sendJson(ws: WebSocket, value: unknown): void {
  ws.send(JSON.stringify(value));
}

function waitOpen(ws: WebSocket): Promise<void> {
  if (ws.readyState === WebSocket.OPEN) return Promise.resolve();
  return new Promise((resolve, reject) => {
    ws.addEventListener("open", () => resolve(), { once: true });
    ws.addEventListener("error", () => reject(new Error("websocket dial failed")), { once: true });
  });
}

async function expectMessage(ws: WebSocket, expectedType: string): Promise<void> {
  const msg = await readJson(ws);
  if (msg.type !== expectedType) {
    throw new Error(`expected ${expectedType}, got ${String(msg.type)}`);
  }
}

function readJson(ws: WebSocket): Promise<Record<string, unknown>> {
  return new Promise((resolve, reject) => {
    const onMessage = (event: MessageEvent) => {
      cleanup();
      try {
        const data =
          typeof event.data === "string" ? event.data : Buffer.from(event.data).toString("utf8");
        resolve(JSON.parse(data) as Record<string, unknown>);
      } catch (error) {
        reject(error);
      }
    };
    const onError = () => {
      cleanup();
      reject(new Error("websocket read failed"));
    };
    const onClose = () => {
      cleanup();
      reject(new Error("websocket closed"));
    };
    const cleanup = () => {
      ws.removeEventListener("message", onMessage);
      ws.removeEventListener("error", onError);
      ws.removeEventListener("close", onClose);
    };
    ws.addEventListener("message", onMessage);
    ws.addEventListener("error", onError);
    ws.addEventListener("close", onClose);
  });
}
