import { voxError } from "./cli-error.ts";

export function isTty(): boolean {
  return Boolean(process.stdin.isTTY && process.stderr.isTTY);
}

export async function promptSecret(label: string): Promise<string> {
  if (!isTty() || typeof process.stdin.setRawMode !== "function") {
    throw voxError(
      "invalid_usage",
      "no terminal available — pass --token instead",
      "vox auth.login --token sk-...",
    );
  }

  process.stderr.write(label);
  const stdin = process.stdin;
  stdin.setRawMode(true);
  stdin.resume();
  stdin.setEncoding("utf8");

  let value = "";
  try {
    await new Promise<void>((resolve, reject) => {
      const onData = (chunk: string | Buffer) => {
        const text = typeof chunk === "string" ? chunk : chunk.toString("utf8");
        for (const ch of text) {
          if (ch === "\r" || ch === "\n") {
            cleanup();
            resolve();
            return;
          }
          if (ch === "\u0003") {
            cleanup();
            reject(voxError("invalid_usage", "login cancelled"));
            return;
          }
          if (ch === "\u007f" || ch === "\b") {
            value = value.slice(0, -1);
            continue;
          }
          if (ch >= " ") value += ch;
        }
      };
      const cleanup = () => {
        stdin.off("data", onData);
      };
      stdin.on("data", onData);
    });
  } finally {
    stdin.setRawMode(false);
    stdin.pause();
    process.stderr.write("\n");
  }
  return value.trim();
}
