// hp-mov4 - Sanitized markdown preview contract for untrusted local-clone files.
//
// Renderer components must consume markdown through this wrapper. Raw HTML is
// escaped by default, links are protocol-filtered, and code text is HTML-escaped
// before it can reach the DOM.

export const ALLOWED_MARKDOWN_PROTOCOLS = ["http:", "https:", "mailto:"] as const;

export interface UnsafeHtmlProjectSetting {
  readonly projectId: string;
  readonly allowUnsafeHtml: boolean;
  readonly warningAcceptedAt: string | null;
}

export interface MarkdownPreviewPolicy {
  readonly rawHtml: "escaped" | "explicit-opt-in-required";
  readonly warningRequired: boolean;
  readonly allowedProtocols: readonly string[];
}

export interface SanitizedMarkdown {
  readonly html: string;
  readonly policy: MarkdownPreviewPolicy;
}

const BASE_POLICY: MarkdownPreviewPolicy = Object.freeze({
  rawHtml: "escaped",
  warningRequired: false,
  allowedProtocols: ALLOWED_MARKDOWN_PROTOCOLS,
});

export function previewPolicy(setting?: UnsafeHtmlProjectSetting | null): MarkdownPreviewPolicy {
  if (!setting?.allowUnsafeHtml) {
    return BASE_POLICY;
  }
  return {
    rawHtml: setting.warningAcceptedAt ? "escaped" : "explicit-opt-in-required",
    warningRequired: !setting.warningAcceptedAt,
    allowedProtocols: ALLOWED_MARKDOWN_PROTOCOLS,
  };
}

export function sanitizeMarkdownPreview(
  markdown: string,
  setting?: UnsafeHtmlProjectSetting | null,
): SanitizedMarkdown {
  const html = renderBlocks(String(markdown));
  return { html, policy: previewPolicy(setting) };
}

export function escapeHtml(value: string): string {
  return value
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#39;");
}

export function sanitizeUrl(value: string): string | null {
  const trimmed = value.trim();
  if (trimmed === "") {
    return null;
  }
  const decoded = decodeHtmlEntitiesRepeated(trimmed);
  const schemeProbe = decoded.replace(/[\u0000-\u0020\u007f]+/g, "").toLowerCase();
  if (schemeProbe.startsWith("//")) {
    return null;
  }
  const scheme = schemeProbe.match(/^([a-z][a-z0-9+.-]*):/i)?.[1];
  if (scheme) {
    const protocol = `${scheme}:`;
    return (ALLOWED_MARKDOWN_PROTOCOLS as readonly string[]).includes(protocol) ? trimmed : null;
  }
  return trimmed;
}

function renderBlocks(markdown: string): string {
  const lines = markdown.replace(/\r\n?/g, "\n").split("\n");
  const blocks: string[] = [];
  let inFence = false;
  let codeLines: string[] = [];

  for (const line of lines) {
    if (line.startsWith("```")) {
      if (inFence) {
        blocks.push(`<pre><code>${escapeHtml(codeLines.join("\n"))}</code></pre>`);
        codeLines = [];
        inFence = false;
      } else {
        inFence = true;
      }
      continue;
    }
    if (inFence) {
      codeLines.push(line);
      continue;
    }
    if (line.trim() === "") {
      continue;
    }
    blocks.push(`<p>${renderInline(line)}</p>`);
  }

  if (inFence) {
    blocks.push(`<pre><code>${escapeHtml(codeLines.join("\n"))}</code></pre>`);
  }
  return blocks.join("\n");
}

function renderInline(line: string): string {
  let out = "";
  let i = 0;
  while (i < line.length) {
    const ch = line[i];
    if (ch === "`") {
      const end = line.indexOf("`", i + 1);
      if (end > i) {
        out += `<code>${escapeHtml(line.slice(i + 1, end))}</code>`;
        i = end + 1;
        continue;
      }
    }
    if (ch === "[") {
      const parsed = tryParseLink(line, i);
      if (parsed) {
        const href = sanitizeUrl(parsed.url);
        if (href) {
          out += `<a href="${escapeHtml(href)}" rel="noreferrer">${renderInline(parsed.label)}</a>`;
        } else {
          out += renderInline(parsed.label);
        }
        i = parsed.end;
        continue;
      }
    }
    out += escapeHtml(ch);
    i += 1;
  }
  return out;
}

function tryParseLink(line: string, start: number): { label: string; url: string; end: number } | null {
  const maxLabelEnd = Math.min(line.length, start + 2048);
  let close = -1;
  for (let i = start + 1; i < maxLabelEnd; i += 1) {
    if (line[i] === "]" && line[i + 1] === "(") {
      close = i;
      break;
    }
  }
  if (close < 0) {
    return null;
  }
  const urlStart = close + 2;
  const maxUrlEnd = Math.min(line.length, urlStart + 2048);
  let urlEnd = -1;
  for (let i = urlStart; i < maxUrlEnd; i += 1) {
    if (line[i] === ")") {
      urlEnd = i;
      break;
    }
  }
  if (urlEnd < 0) {
    return null;
  }
  return {
    label: line.slice(start + 1, close),
    url: line.slice(urlStart, urlEnd),
    end: urlEnd + 1,
  };
}

function decodeHtmlEntitiesRepeated(value: string): string {
  let current = value;
  for (let i = 0; i < 3; i += 1) {
    const next = decodeHtmlEntities(current);
    if (next === current) {
      return next;
    }
    current = next;
  }
  return current;
}

function decodeHtmlEntities(value: string): string {
  return value.replace(/&(#x[0-9a-f]+|#\d+|colon|tab|newline|amp);/gi, (_all, entity: string) => {
    const lower = entity.toLowerCase();
    if (lower.startsWith("#x")) {
      return charFromCode(Number.parseInt(lower.slice(2), 16));
    }
    if (lower.startsWith("#")) {
      return charFromCode(Number.parseInt(lower.slice(1), 10));
    }
    switch (lower) {
      case "colon":
        return ":";
      case "tab":
        return "\t";
      case "newline":
        return "\n";
      case "amp":
        return "&";
      default:
        return "";
    }
  });
}

function charFromCode(code: number): string {
  if (!Number.isFinite(code) || code <= 0 || code > 0x10ffff) {
    return "";
  }
  return String.fromCodePoint(code);
}
