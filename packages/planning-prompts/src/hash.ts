import { createHash } from "node:crypto";

export function computePromptHash(body: string): string {
  const normalized = body.replace(/\r\n/g, "\n").endsWith("\n")
    ? body.replace(/\r\n/g, "\n")
    : `${body.replace(/\r\n/g, "\n")}\n`;
  return `sha256:${createHash("sha256").update(normalized, "utf8").digest("hex")}`;
}
