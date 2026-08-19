import { existsSync } from "node:fs";
import { readFile } from "node:fs/promises";
import { resolve } from "node:path";

import { voxError } from "./cli-error.ts";

/** Field-level sources for long free text: inline | `-` | `@path` (if it exists). */
export async function resolveTextField(
  raw: string | undefined,
  flag = "--text",
): Promise<string | undefined> {
  if (raw === undefined) return undefined;
  const trimmed = raw.trim();
  if (trimmed === "-") {
    const body = await readStdin();
    if (!body.trim()) {
      throw voxError(
        "invalid_usage",
        `text from stdin is empty. Use: vox say ${flag} - <<'TXT' … TXT`,
      );
    }
    return body;
  }
  const filePath = parseFileRef(trimmed);
  if (filePath !== undefined) {
    try {
      return await readFile(filePath, "utf8");
    } catch (error) {
      throw voxError(
        "io_error",
        `cannot read ${filePath}: ${error instanceof Error ? error.message : String(error)}`,
      );
    }
  }
  return raw;
}

export function parseFileRef(value: string, cwd = process.cwd()): string | undefined {
  const trimmed = value.trim();
  if (!trimmed.startsWith("@")) return undefined;
  const path = trimmed.slice(1).trim();
  if (!path) return undefined;
  const absolute = resolve(cwd, path);
  return existsSync(absolute) ? absolute : undefined;
}

/**
 * `vox say --voice Cherry @script.txt` looks like a file body, but argc treats
 * a bare `@` as whole-command JSON. Rewrite only when flags are already present.
 * Leave `vox say @payload.json` (sole token) alone.
 */
export function rewriteSayTextArgv(argv: string[]): string[] {
  if (argv[0] !== "say") return argv;
  const rest = argv.slice(1);
  if (rest.length < 2) return argv;
  if (rest.includes("--text")) return argv;
  const out = ["say"];
  let rewritten = false;
  for (const token of rest) {
    if (!rewritten && token.startsWith("@") && parseFileRef(token)) {
      out.push("--text", token);
      rewritten = true;
      continue;
    }
    out.push(token);
  }
  return out;
}

async function readStdin(): Promise<string> {
  const chunks: Buffer[] = [];
  for await (const chunk of process.stdin) {
    chunks.push(typeof chunk === "string" ? Buffer.from(chunk) : chunk);
  }
  return Buffer.concat(chunks).toString("utf8");
}
