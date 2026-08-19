import { writeFile } from "node:fs/promises";

import { voxError } from "./cli-error.ts";
import {
  clearCredentials,
  loadConfig,
  requireApiKey,
  saveConfig,
  type AppConfig,
} from "./config.ts";
import { DashScopeClient, resolveModel, VOCABULARY_QUOTA } from "./dashscope.ts";
import { render } from "./export.ts";
import { hear } from "./hear.ts";
import { promptSecret } from "./prompt.ts";
import { listRuns, loadRun, newStore, removeAllRuns, removeRun, resolveSid } from "./run-store.ts";
import type { AppHandlers } from "./schema.ts";
import { cacheClear, cacheStatus, listVoices, recordVoice, removeVoice, say } from "./tts.ts";
import {
  describeIndex,
  loadIndex,
  loadVocabulary,
  listVocabularies,
  pruneVocabularies,
  syncVocabulary,
  vocabNames,
  vocabPath,
  tilde,
} from "./vocab.ts";

export const handlers: AppHandlers = {
  status: async () => await authStatus(),
  auth: {
    login: async (options) => await authLogin(options.input.token),
    logout: async () => await authLogout(),
    status: async () => await authStatus(),
  },
  hear: async (options) => {
    const app = await loadConfig();
    return await hear(app, options.input);
  },
  session: {
    list: async (options) => {
      const app = await loadConfig();
      return await listRuns(newStore(app.dir), options.input.file ?? "", options.input.limit ?? 20);
    },
    remove: async (options) => await sessionRemove(options.input.sid, options.input.all ?? false),
  },
  export: async (options) => await exportRun(options.input),
  vocab: {
    list: async () => await vocabList(),
    sync: async (options) => await vocabSync(options.input),
    prune: async (options) => await vocabPrune(options.input.dryRun ?? false),
  },
  say: async (options) => {
    const app = await loadConfig();
    return await say(app, options.input);
  },
  voice: {
    list: async () => {
      const app = await loadConfig();
      return await listVoices(app);
    },
    record: async (options) => {
      const app = await loadConfig();
      return await recordVoice(app, options.input);
    },
    remove: async (options) => {
      const app = await loadConfig();
      return await removeVoice(app, options.input.voiceId);
    },
  },
  cache: {
    status: async () => {
      const app = await loadConfig();
      return await cacheStatus(app);
    },
    clear: async () => {
      const app = await loadConfig();
      return await cacheClear(app);
    },
  },
};

async function authStatus(): Promise<Record<string, unknown>> {
  const app = await loadConfig();
  const key = app.config.services.dashscope?.api_key;
  if (!key) return { authenticated: false };
  return { service: "dashscope" };
}

async function authLogin(token: string | undefined): Promise<Record<string, unknown>> {
  let value = token?.trim() ?? "";
  if (!value) value = await promptSecret("DashScope API Key: ");
  if (!value)
    throw voxError("invalid_usage", "API key is required", "vox auth.login --token sk-...");

  console.error("validating...");
  try {
    await new DashScopeClient(value).validate();
  } catch (error) {
    throw voxError(
      "api_error",
      `invalid credentials: ${error instanceof Error ? error.message : String(error)}`,
    );
  }

  const app = await loadConfig();
  app.config.services.dashscope = { api_key: value };
  await saveConfig(app);
  return { service: "dashscope" };
}

async function authLogout(): Promise<Record<string, unknown>> {
  const app = await loadConfig();
  if (!app.config.services.dashscope?.api_key) return { cleared: false };
  await clearCredentials(app);
  return { cleared: true };
}

async function sessionRemove(
  sid: string | undefined,
  all: boolean,
): Promise<Record<string, unknown>> {
  const app = await loadConfig();
  const store = newStore(app.dir);
  if (all && sid) {
    throw voxError(
      "invalid_usage",
      "--all cannot be combined with a run id",
      `vox session.remove --sid ${sid}  ·  vox session.remove --all`,
    );
  }
  if (all) {
    await removeAllRuns(store);
    return { removed: "all" };
  }
  if (!sid) {
    throw voxError(
      "invalid_usage",
      "no run id given",
      "vox session.remove --sid <sid>  ·  vox session.remove --all",
    );
  }
  const resolved = await resolveSid(store, sid);
  await removeRun(store, resolved);
  return { removed: resolved };
}

async function exportRun(input: {
  sid: string;
  format: "srt" | "vtt" | "md" | "txt" | "json";
  output?: string;
}): Promise<unknown> {
  const app = await loadConfig();
  const store = newStore(app.dir);
  const sid = await resolveSid(store, input.sid);
  let rec;
  try {
    rec = await loadRun(store, sid);
  } catch (error) {
    throw voxError(
      "session_not_found",
      `run ${sid} has no stored result: ${error instanceof Error ? error.message : String(error)}`,
    );
  }
  const rendered = render(rec, input.format);
  if (input.output) {
    try {
      await writeFile(input.output, rendered);
    } catch (error) {
      throw voxError(
        "io_error",
        `cannot write ${input.output}: ${error instanceof Error ? error.message : String(error)}`,
      );
    }
    return { written: input.output, format: input.format, sid };
  }
  return rendered;
}

async function vocabList(): Promise<unknown> {
  const app = await loadConfig();
  const names = await vocabNames(app.dir);
  const index = await loadIndex(app.dir);
  const vocabularies = [];
  for (const name of names) {
    const entry: Record<string, unknown> = { name, path: tilde(vocabPath(app.dir, name)) };
    try {
      const vocab = await loadVocabulary(app.dir, name);
      entry.words = Object.keys(vocab.file.words).length;
      if (vocab.file.lang) entry.lang = vocab.file.lang;
      const synced = describeIndex(index, name);
      if (Object.keys(synced).length > 0) entry.synced = synced;
    } catch (error) {
      entry.error = error instanceof Error ? error.message : String(error);
    }
    vocabularies.push(entry);
  }
  return { quota: await quotaUsage(app), vocabularies };
}

async function quotaUsage(app: AppConfig): Promise<string> {
  try {
    const key = requireApiKey(app);
    const remote = await new DashScopeClient(key).listVocabularies();
    return `${remote.length}/${VOCABULARY_QUOTA}`;
  } catch (error) {
    if (error instanceof Error && error.name === "not_authenticated") {
      return "unknown (not authenticated)";
    }
    return `unknown (${error instanceof Error ? error.message : String(error)})`;
  }
}

async function vocabSync(input: {
  name?: string;
  all?: boolean;
  model?: string;
  force?: boolean;
}): Promise<unknown> {
  const app = await loadConfig();
  const apiKey = requireApiKey(app);
  const vendor = input.model ?? "fun";
  const model = resolveModel(vendor);
  if (!model) throw voxError("invalid_usage", `unknown model vendor "${vendor}"`);
  if (input.all && input.name) {
    throw voxError("invalid_usage", "--all cannot be combined with a vocabulary name");
  }
  if (!input.all && !input.name) {
    throw voxError(
      "invalid_usage",
      "no vocabulary name given",
      "vox vocab.sync --name meeting  ·  vox vocab.sync --all",
    );
  }

  const client = new DashScopeClient(apiKey);
  const targets = input.all
    ? await listVocabularies(app.dir)
    : [await loadVocabulary(app.dir, input.name!)];
  const items = [];
  for (const vocab of targets) {
    const result = await syncVocabulary(client, app.dir, vocab, model, input.force ?? false);
    for (const warning of result.warnings) console.error(`${vocab.name}: ${warning}`);
    items.push({
      name: vocab.name,
      action: result.action,
      vocabularyId: result.vocabularyId,
      warnings: result.warnings,
    });
  }
  return { items };
}

async function vocabPrune(dryRun: boolean): Promise<unknown> {
  const app = await loadConfig();
  const apiKey = requireApiKey(app);
  const orphans = await pruneVocabularies(new DashScopeClient(apiKey), app.dir, dryRun);
  return { orphans, dryRun };
}
