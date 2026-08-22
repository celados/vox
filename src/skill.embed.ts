import { pickFiles } from "@celados/argc/skill";

export function embedSkill(): Record<string, string> {
  return pickFiles(import.meta.dir, ["SKILL.md"]);
}
