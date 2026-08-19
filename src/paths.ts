import { homedir } from "node:os";
import { join } from "node:path";

const APP_DIR = ".vox";

export function voxHome(): string {
  return join(homedir(), APP_DIR);
}

export function expandHome(path: string): string {
  if (path.startsWith("~/")) return join(homedir(), path.slice(2));
  return path;
}

export function tilde(path: string): string {
  const home = homedir();
  if (path === home) return "~";
  if (path.startsWith(home + "/")) return "~" + path.slice(home.length);
  return path;
}
