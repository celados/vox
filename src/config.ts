import { chmod, mkdir, readFile, writeFile } from "node:fs/promises";
import { join } from "node:path";

import { voxError } from "./cli-error.ts";
import { voxHome } from "./paths.ts";

export type DashScopeConfig = {
  api_key?: string;
};

export type AppFile = {
  services: {
    dashscope?: DashScopeConfig;
  };
};

export type StateFile = {
  last_voice?: string;
  last_lang?: string;
};

export type AppConfig = {
  dir: string;
  config: AppFile;
  state: StateFile;
};

const emptyConfig = (): AppFile => ({ services: {} });

export async function loadConfig(dir = voxHome()): Promise<AppConfig> {
  await mkdir(dir, { recursive: true });
  await mkdir(join(dir, "voices"), { recursive: true });
  await mkdir(join(dir, "cache"), { recursive: true });

  const config = await readJson<AppFile>(join(dir, "config.json"), emptyConfig());
  const state = await readJson<StateFile>(join(dir, "state.json"), {});
  return { dir, config, state };
}

export async function saveConfig(app: AppConfig): Promise<void> {
  await writeSecretJson(join(app.dir, "config.json"), app.config);
}

export async function saveState(app: AppConfig): Promise<void> {
  await writeSecretJson(join(app.dir, "state.json"), app.state);
}

export function requireApiKey(app: AppConfig): string {
  const key = app.config.services.dashscope?.api_key;
  if (!key) {
    throw voxError("not_authenticated", "no DashScope credentials stored", "vox auth.login");
  }
  return key;
}

export async function clearCredentials(app: AppConfig): Promise<void> {
  app.config.services = {};
  await saveConfig(app);
}

async function readJson<T>(path: string, fallback: T): Promise<T> {
  let text: string;
  try {
    text = await readFile(path, "utf8");
  } catch (error) {
    if (isNotFound(error)) return fallback;
    throw voxError(
      "io_error",
      `cannot read ${path}: ${error instanceof Error ? error.message : String(error)}`,
    );
  }
  try {
    return JSON.parse(text) as T;
  } catch (error) {
    throw voxError(
      "io_error",
      `${path} is not readable: ${error instanceof Error ? error.message : String(error)}`,
    );
  }
}

async function writeSecretJson(path: string, value: unknown): Promise<void> {
  await writeFile(path, JSON.stringify(value, null, 2) + "\n", { mode: 0o600 });
  await chmod(path, 0o600);
}

function isNotFound(error: unknown): boolean {
  return typeof error === "object" && error !== null && "code" in error && error.code === "ENOENT";
}
