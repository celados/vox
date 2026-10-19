import { createHash } from "node:crypto";
import { mkdir, rename, writeFile } from "node:fs/promises";
import { join, resolve } from "node:path";

import runtime from "../python/runtime.py" with { type: "text" };
import project from "../python/pyproject.toml" with { type: "text" };
import lock from "../python/uv.lock" with { type: "text" };
import license from "../python/LICENSE.wespeaker" with { type: "text" };
import { voxError } from "./cli-error.ts";
import { expandHome, tilde } from "./paths.ts";
import {
  normalized,
  SPEAKER_MODEL,
  type AudioSegment,
  type SpeakerEmbedding,
} from "./speaker-match.ts";

export function checkSpeakerRuntime(): void {
  if (process.platform !== "darwin" || process.arch !== "arm64") {
    throw voxError(
      "speaker_runtime_unavailable",
      "Local speaker identification requires Apple Silicon macOS",
    );
  }
  for (const tool of ["uv", "ffmpeg"]) {
    if (!Bun.which(tool))
      throw voxError(
        "speaker_runtime_unavailable",
        `${tool} is required for local speaker identification`,
        `brew install ${tool}`,
      );
  }
}

export async function embedAudio(
  home: string,
  audio: string,
  segments: AudioSegment[],
): Promise<SpeakerEmbedding[]> {
  checkSpeakerRuntime();
  const hash = createHash("sha256")
    .update(runtime)
    .update(project)
    .update(lock)
    .update(license)
    .digest("hex")
    .slice(0, 16);
  const dir = join(home, "runtime", hash);
  await mkdir(dir, { recursive: true, mode: 0o700 });
  for (const [name, text] of [
    ["runtime.py", runtime],
    ["pyproject.toml", project],
    ["uv.lock", lock],
    ["LICENSE.wespeaker", license],
  ]) {
    const path = join(dir, name!);
    if (
      (await Bun.file(path)
        .text()
        .catch(() => "")) === text
    )
      continue;
    const tmp = `${path}.${crypto.randomUUID()}.tmp`;
    await writeFile(tmp, text!, { mode: 0o600 });
    await rename(tmp, path);
  }
  console.error(
    `speaker model: local MLX (first use downloads runtime and weights into ${tilde(home)})`,
  );
  const proc = Bun.spawn(
    ["uv", "run", "--locked", "--project", dir, "python", join(dir, "runtime.py")],
    {
      stdin: "pipe",
      stdout: "pipe",
      stderr: "inherit",
      env: {
        ...process.env,
        VOX_HOME: home,
        UV_CACHE_DIR: join(home, "cache", "uv"),
        UV_PYTHON_INSTALL_DIR: join(home, "runtime", "python"),
        UV_PROJECT_ENVIRONMENT: join(dir, ".venv"),
      },
    },
  );
  proc.stdin.write(JSON.stringify({ home, audio: resolve(expandHome(audio)), segments }));
  proc.stdin.end();
  const [stdout, status] = await Promise.all([new Response(proc.stdout).text(), proc.exited]);
  let parsed: unknown;
  try {
    parsed = JSON.parse(stdout);
  } catch {
    throw voxError(
      "speaker_runtime_unavailable",
      `Speaker runtime exited ${status} without a JSON result; see stderr`,
    );
  }
  if (status !== 0) {
    const detail =
      typeof parsed === "object" && parsed !== null && "error" in parsed
        ? JSON.stringify(parsed.error)
        : `exit ${status}`;
    throw voxError("speaker_runtime_unavailable", `Speaker runtime failed: ${detail}`);
  }
  if (
    !parsed ||
    typeof parsed !== "object" ||
    !("model" in parsed) ||
    parsed.model !== SPEAKER_MODEL ||
    !("embeddings" in parsed) ||
    !Array.isArray(parsed.embeddings)
  ) {
    throw voxError(
      "speaker_runtime_unavailable",
      "Speaker runtime returned an invalid model or response",
    );
  }
  try {
    if (parsed.embeddings.length !== segments.length)
      throw new Error("Embedding count does not match requested segments");
    return parsed.embeddings.map((raw: unknown, index: number) => {
      if (
        !raw ||
        typeof raw !== "object" ||
        !("id" in raw) ||
        typeof raw.id !== "string" ||
        !("vector" in raw) ||
        !Array.isArray(raw.vector) ||
        raw.vector.some((n: unknown) => typeof n !== "number") ||
        !("duration" in raw) ||
        typeof raw.duration !== "number" ||
        !Number.isFinite(raw.duration) ||
        raw.duration < 3 ||
        raw.id !== segments[index]?.id
      )
        throw new Error("Invalid embedding entry");
      return { id: raw.id, vector: normalized(raw.vector), duration: raw.duration };
    });
  } catch (error) {
    throw voxError("speaker_runtime_unavailable", `Invalid speaker embeddings: ${String(error)}`);
  }
}
