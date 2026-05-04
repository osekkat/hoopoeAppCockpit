import { existsSync, readFileSync } from "node:fs";
import { basename, dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

export const CONFORMANCE_TOOLS = [
  "br",
  "bv",
  "ntm",
  "agent_mail",
  "git",
  "ru",
  "rch",
  "caam",
  "dcg",
  "caut",
  // hp-8a8: §18.3 contract-test scope extended to the remaining adapters.
  // `health` is the umbrella adapter; per-language `health_<lang>` split
  // is deferred until the real-VPS pinning bead lands per-language
  // fixtures (the capability registry already namespaces `health_<lang>`
  // per hp-r33).
  "health",
  "casr",
  "pt",
  "srp",
  "sbh",
  "ubs",
  "jsm",
  "jfp",
  "oracle",
] as const;

export type ConformanceTool = (typeof CONFORMANCE_TOOLS)[number];

type CapabilityStatus = "ok" | "degraded" | "missing" | "blocked-by-policy" | "untested";

interface CapabilityEntry {
  status: CapabilityStatus;
  notes?: string;
  fallback?: string;
  transport?: string;
}

interface GoldenEnvelope {
  meta: {
    adapter: string;
    state: string;
    kind: string;
    fixturesVersion: string;
    capturedAt: string;
    source: string;
  };
  argv: string[];
  exit: number;
  durationMs: number;
  stdoutBytes?: number;
  stderrBytes?: number;
  stdoutJson?: unknown;
  stdoutText?: string | null;
  stderrText?: string | null;
  truncated?: boolean;
  redacted?: boolean;
  tags?: string[];
  capabilities?: Record<string, CapabilityEntry>;
}

export interface NormalizedOutput {
  tool: ConformanceTool;
  source: "fixture" | "local";
  state: string;
  exit: number;
  argv: string[];
  capabilities: Record<string, CapabilityEntry>;
  payload: Record<string, unknown>;
}

type FindingSeverity = "error" | "warning";

export interface ConformanceFinding {
  id: string;
  tool: ConformanceTool;
  severity: FindingSeverity;
  expected: boolean;
  message: string;
  where: string;
}

export interface ToolConformanceReport {
  tool: ConformanceTool;
  findings: ConformanceFinding[];
  schemaPath: string;
  fixturePath?: string;
  cases: {
    schema: "pass" | "fail";
    roundTrip: "pass" | "fail";
    negative: "pass" | "fail";
    capabilities: "pass" | "fail";
  };
}

export interface ConformanceReport {
  tools: ToolConformanceReport[];
  findings: ConformanceFinding[];
  unexpectedFindings: ConformanceFinding[];
  expectedFindings: ConformanceFinding[];
}

export class ConformanceParseError extends Error {
  override readonly name = "ConformanceParseError";
  readonly code: string;
  readonly tool: ConformanceTool;

  constructor(tool: ConformanceTool, code: string, message: string) {
    super(message);
    this.tool = tool;
    this.code = code;
  }
}

const here = dirname(fileURLToPath(import.meta.url));
const fixturesPackageRoot = resolve(here, "..");

const EXPECTED_FINDING_IDS: Record<ConformanceTool, readonly string[]> = {
  br: [],
  bv: [],
  ntm: [],
  agent_mail: [
    "agent_mail.capability.unsatisfied.agent_mail.messages.read",
    "agent_mail.capability.unsatisfied.agent_mail.messages.send",
    "agent_mail.capability.unsatisfied.agent_mail.reservations.list",
  ],
  git: [],
  ru: [],
  rch: [],
  caam: ["caam.capability.unsatisfied.caam.accounts.list"],
  dcg: [],
  caut: ["caut.capability.missing.caut.usage.snapshot"],
  // hp-8a8: stub-fixture adapters carry no declared capabilities yet, so
  // no capability rules fire and no drift is expected.
  health: [],
  casr: [],
  pt: [],
  srp: [],
  sbh: [],
  ubs: [],
  jsm: [],
  jfp: [],
  oracle: [],
};

function schemaPath(tool: ConformanceTool): string {
  return resolve(here, "schemas", `${tool}.schema.json`);
}

function goldenPath(tool: ConformanceTool, state: string): string {
  return resolve(fixturesPackageRoot, "golden-outputs", tool, `${state}.json`);
}

function isObject(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function parseJsonFile(path: string): unknown {
  return JSON.parse(readFileSync(path, "utf8")) as unknown;
}

function parseEnvelopeText(tool: ConformanceTool, text: string, where: string): GoldenEnvelope {
  if (text.trim().length === 0) {
    throw new ConformanceParseError(tool, "empty_input", `${where} is empty`);
  }
  let parsed: unknown;
  try {
    parsed = JSON.parse(text) as unknown;
  } catch (error) {
    throw new ConformanceParseError(
      tool,
      "invalid_json",
      `${where} is not valid JSON: ${error instanceof Error ? error.message : String(error)}`,
    );
  }
  if (!isObject(parsed)) {
    throw new ConformanceParseError(tool, "non_object", `${where} must parse to an object`);
  }
  const meta = parsed.meta;
  if (!isObject(meta) || meta.adapter !== tool || typeof meta.state !== "string") {
    throw new ConformanceParseError(tool, "bad_meta", `${where} has invalid fixture metadata`);
  }
  if (
    !Array.isArray(parsed.argv) ||
    typeof parsed.exit !== "number" ||
    typeof parsed.durationMs !== "number"
  ) {
    throw new ConformanceParseError(
      tool,
      "bad_envelope",
      `${where} is missing argv/exit/durationMs`,
    );
  }
  return parsed as unknown as GoldenEnvelope;
}

function loadEnvelope(tool: ConformanceTool, state: string): GoldenEnvelope {
  const path = goldenPath(tool, state);
  const text = readFileSync(path, "utf8");
  return parseEnvelopeText(tool, text, `${tool}/${state}`);
}

function canonicalize(value: unknown): unknown {
  if (Array.isArray(value)) return value.map(canonicalize);
  if (!isObject(value)) return value;
  const out: Record<string, unknown> = {};
  for (const key of Object.keys(value).sort()) {
    const child = value[key];
    if (child !== undefined) out[key] = canonicalize(child);
  }
  return out;
}

function canonicalJson(value: unknown): string {
  return JSON.stringify(canonicalize(value));
}

function requireStdoutObject(
  tool: ConformanceTool,
  envelope: GoldenEnvelope,
): Record<string, unknown> {
  if (isObject(envelope.stdoutJson)) return envelope.stdoutJson;
  throw new ConformanceParseError(
    tool,
    "missing_json_payload",
    `${tool} fixture has no JSON payload`,
  );
}

function parseGitPorcelain(stdoutText: string): Record<string, unknown> {
  const branch: Record<string, unknown> = {};
  const entries: string[] = [];
  for (const line of stdoutText.split("\n")) {
    if (line.startsWith("# branch.oid ")) branch.oid = line.slice("# branch.oid ".length);
    else if (line.startsWith("# branch.head ")) branch.head = line.slice("# branch.head ".length);
    else if (line.startsWith("# branch.upstream "))
      branch.upstream = line.slice("# branch.upstream ".length);
    else if (line.startsWith("# branch.ab ")) branch.ab = line.slice("# branch.ab ".length);
    else if (line.trim().length > 0) entries.push(line);
  }
  return { branch, entries };
}

function parseRchStatus(stdoutText: string): Record<string, unknown> {
  const lineValue = (label: string): string => {
    const line = stdoutText
      .split("\n")
      .find((candidate) => candidate.trim().startsWith(`${label} :`));
    return line?.split(":").slice(1).join(":").trim() ?? "";
  };
  return {
    statusText: stdoutText,
    daemon: lineValue("Daemon"),
    posture: lineValue("Posture"),
    workers: lineValue("Workers"),
    builds: lineValue("Builds"),
  };
}

// hp-b6r2: br conformance previously passed raw `br --json` output through
// untouched, validating snake_case fields (issues / has_more / issue_type)
// against a local schema that didn't reflect the OpenAPI BeadListResponse
// contract. The daemon's br adapter must surface camelCase Bead objects
// with a SchemaVersion (per packages/schemas/openapi.yaml BeadListResponse
// + Bead). This mapper bridges raw br stdout into that daemon-facing shape
// so the conformance harness validates what the API actually emits, not
// what br emits to its CLI consumers.
//
// The raw envelope still lives in golden-outputs/br/normal.json — it is
// the *input* fixture; this function produces the *output* the harness
// then schema-checks.
export const BR_BEAD_SCHEMA_VERSION = 1;

interface BeadListResponseShape {
  items: BeadShape[];
  page: { hasMore: boolean; total?: number };
}

interface BeadShape {
  schemaVersion: number;
  id: string;
  title: string;
  status: string;
  priority: number;
  issueType: string;
  description?: string;
  sourceRepo?: string;
  createdAt?: string;
  updatedAt?: string;
  createdBy?: string;
}

export function mapBrToBeadListResponse(raw: Record<string, unknown>): Record<string, unknown> {
  const issuesRaw = Array.isArray(raw.issues) ? raw.issues : [];
  const items: BeadShape[] = [];
  for (const entry of issuesRaw) {
    if (!isObject(entry)) continue;
    const bead = mapBrIssueToBead(entry);
    if (bead !== null) items.push(bead);
  }
  const page: BeadListResponseShape["page"] = {
    hasMore: typeof raw.has_more === "boolean" ? raw.has_more : false,
  };
  if (typeof raw.total === "number" && Number.isInteger(raw.total) && raw.total >= 0) {
    page.total = raw.total;
  }
  const response: BeadListResponseShape = { items, page };
  return response as unknown as Record<string, unknown>;
}

function mapBrIssueToBead(raw: Record<string, unknown>): BeadShape | null {
  if (typeof raw.id !== "string" || raw.id.length === 0) return null;
  if (typeof raw.title !== "string" || raw.title.length === 0) return null;
  if (typeof raw.status !== "string") return null;
  if (typeof raw.priority !== "number" || !Number.isInteger(raw.priority)) return null;
  if (typeof raw.issue_type !== "string") return null;
  const bead: BeadShape = {
    schemaVersion: BR_BEAD_SCHEMA_VERSION,
    id: raw.id,
    title: raw.title,
    status: raw.status,
    priority: raw.priority,
    issueType: raw.issue_type,
  };
  if (typeof raw.description === "string") bead.description = raw.description;
  if (typeof raw.source_repo === "string") bead.sourceRepo = raw.source_repo;
  if (typeof raw.created_at === "string") bead.createdAt = raw.created_at;
  if (typeof raw.updated_at === "string") bead.updatedAt = raw.updated_at;
  if (typeof raw.created_by === "string") bead.createdBy = raw.created_by;
  return bead;
}

function normalizeOutput(
  tool: ConformanceTool,
  envelope: GoldenEnvelope,
  source: "fixture" | "local",
): NormalizedOutput {
  if (envelope.truncated) {
    throw new ConformanceParseError(tool, "truncated_output", `${tool} output was truncated`);
  }
  if (envelope.meta.state === "malformed-json") {
    throw new ConformanceParseError(
      tool,
      "tool_output_malformed",
      `${tool} malformed fixture must not parse`,
    );
  }

  let payload: Record<string, unknown>;
  switch (tool) {
    case "br":
      payload = mapBrToBeadListResponse(requireStdoutObject(tool, envelope));
      break;
    case "bv": {
      const raw = requireStdoutObject(tool, envelope);
      const triage = isObject(raw.triage) ? raw.triage : raw;
      payload = {
        bottlenecks: Array.isArray(triage.blockers_to_clear) ? triage.blockers_to_clear : [],
        recommendations: Array.isArray(triage.recommendations) ? triage.recommendations : [],
        summary: isObject(triage.project_health) ? triage.project_health : {},
        rawFormat: isObject(raw.triage) ? "v0.16-triage-envelope" : "legacy-top-level",
      };
      break;
    }
    case "ntm": {
      const raw = requireStdoutObject(tool, envelope);
      payload = {
        success: raw.success ?? true,
        sessions: raw.sessions ?? null,
        tools: Array.isArray(raw.tools) ? raw.tools : [],
        agent_mail: raw.agent_mail ?? null,
      };
      break;
    }
    case "agent_mail":
      payload = {
        helpBanner: (envelope.stdoutText ?? "").includes("MCP Agent Mail"),
        mcpEvidence: isObject(envelope.stdoutJson),
        stdoutText: envelope.stdoutText ?? "",
      };
      break;
    case "git":
      payload = parseGitPorcelain(envelope.stdoutText ?? "");
      break;
    case "ru": {
      const raw = requireStdoutObject(tool, envelope);
      const data = isObject(raw.data) ? raw.data : {};
      const content = isObject(data.content) ? data.content : {};
      payload = {
        schemaVersion: data.schema_version,
        commands: isObject(content.commands) ? content.commands : {},
        envelope: content.envelope ?? null,
      };
      break;
    }
    case "rch":
      payload = parseRchStatus(envelope.stdoutText ?? "");
      break;
    case "caam":
      payload = {
        commandAccepted: envelope.exit === 0,
        accounts: Array.isArray(
          (envelope.stdoutJson as { accounts?: unknown } | undefined)?.accounts,
        )
          ? (envelope.stdoutJson as { accounts: unknown }).accounts
          : null,
        stderrText: envelope.stderrText ?? "",
      };
      break;
    case "dcg":
      payload = {
        probe: {
          statusCommandAccepted: envelope.exit === 0,
          stderrText: envelope.stderrText ?? "",
        },
      };
      break;
    case "caut":
      payload = {
        usage: isObject(envelope.stdoutJson) ? envelope.stdoutJson : {},
        providers: Array.isArray(
          (envelope.stdoutJson as { providers?: unknown } | undefined)?.providers,
        )
          ? (envelope.stdoutJson as { providers: unknown }).providers
          : null,
      };
      break;
    // hp-8a8: stub-shape adapters share a uniform normalized payload so
    // the schema-validator runs against a stable shape regardless of how
    // sparse the underlying fixture is (most are `{}` stdoutJson today;
    // realistic captures will fill in stderr/stdout text).
    case "casr":
    case "pt":
    case "srp":
    case "sbh":
    case "jfp":
    case "oracle":
    case "health":
      payload = {
        commandAccepted: envelope.exit === 0,
        stdoutJson: envelope.stdoutJson ?? null,
        stdoutText: envelope.stdoutText ?? "",
        stderrText: envelope.stderrText ?? "",
      };
      break;
    case "ubs": {
      const text = `${envelope.stderrText ?? ""}\n${envelope.stdoutText ?? ""}`;
      payload = {
        commandAccepted: envelope.exit === 0,
        helpBanner: text.includes("Usage: ubs"),
        stderrText: envelope.stderrText ?? "",
      };
      break;
    }
    case "jsm": {
      const json = isObject(envelope.stdoutJson) ? envelope.stdoutJson : null;
      const skills = Array.isArray((json as { skills?: unknown } | null)?.skills)
        ? ((json as { skills: unknown }).skills as unknown[])
        : null;
      const workspace =
        typeof (json as { workspace?: unknown } | null)?.workspace === "string"
          ? (json as { workspace: string }).workspace
          : null;
      payload = {
        commandAccepted: envelope.exit === 0,
        skills,
        workspace,
      };
      break;
    }
  }

  return {
    tool,
    source,
    state: envelope.meta.state,
    exit: envelope.exit,
    argv: envelope.argv,
    capabilities: envelope.capabilities ?? {},
    payload,
  };
}

interface JsonSchema {
  const?: unknown;
  type?: string | string[];
  enum?: unknown[];
  required?: string[];
  properties?: Record<string, JsonSchema>;
  items?: JsonSchema;
  additionalProperties?: boolean;
  minItems?: number;
  minLength?: number;
  minimum?: number;
  pattern?: string;
  anyOf?: JsonSchema[];
}

function matchesType(value: unknown, type: string): boolean {
  switch (type) {
    case "array":
      return Array.isArray(value);
    case "boolean":
      return typeof value === "boolean";
    case "integer":
      return typeof value === "number" && Number.isInteger(value);
    case "null":
      return value === null;
    case "number":
      return typeof value === "number";
    case "object":
      return isObject(value);
    case "string":
      return typeof value === "string";
    default:
      return true;
  }
}

function validateSchema(value: unknown, schema: JsonSchema, path = "$"): string[] {
  if (schema.anyOf) {
    const candidates = schema.anyOf.map((candidate) => validateSchema(value, candidate, path));
    if (candidates.some((errors) => errors.length === 0)) return [];
    return [`${path} did not match anyOf: ${candidates.map((errors) => errors[0]).join("; ")}`];
  }
  if ("const" in schema && JSON.stringify(value) !== JSON.stringify(schema.const)) {
    return [
      `${path} expected const ${JSON.stringify(schema.const)} but got ${JSON.stringify(value)}`,
    ];
  }
  if (
    schema.enum &&
    !schema.enum.some((entry) => JSON.stringify(entry) === JSON.stringify(value))
  ) {
    return [`${path} expected one of ${schema.enum.join(", ")} but got ${JSON.stringify(value)}`];
  }
  const types = schema.type ? (Array.isArray(schema.type) ? schema.type : [schema.type]) : [];
  if (types.length > 0 && !types.some((type) => matchesType(value, type))) {
    return [
      `${path} expected ${types.join("|")} but got ${Array.isArray(value) ? "array" : typeof value}`,
    ];
  }

  const errors: string[] = [];
  if (typeof value === "string") {
    if (schema.minLength !== undefined && value.length < schema.minLength) {
      errors.push(`${path} expected length >= ${schema.minLength}`);
    }
    if (schema.pattern && !new RegExp(schema.pattern).test(value)) {
      errors.push(`${path} did not match /${schema.pattern}/`);
    }
  }
  if (typeof value === "number" && schema.minimum !== undefined && value < schema.minimum) {
    errors.push(`${path} expected >= ${schema.minimum}`);
  }
  if (Array.isArray(value)) {
    if (schema.minItems !== undefined && value.length < schema.minItems) {
      errors.push(`${path} expected at least ${schema.minItems} items`);
    }
    if (schema.items) {
      value.forEach((item, index) =>
        errors.push(...validateSchema(item, schema.items!, `${path}[${index}]`)),
      );
    }
  }
  if (isObject(value)) {
    for (const key of schema.required ?? []) {
      if (!(key in value)) errors.push(`${path}.${key} is required`);
    }
    for (const [key, childSchema] of Object.entries(schema.properties ?? {})) {
      if (key in value) errors.push(...validateSchema(value[key], childSchema, `${path}.${key}`));
    }
    if (schema.additionalProperties === false && schema.properties) {
      for (const key of Object.keys(value)) {
        if (!(key in schema.properties)) errors.push(`${path}.${key} is not allowed`);
      }
    }
  }
  return errors;
}

function finding(
  tool: ConformanceTool,
  id: string,
  message: string,
  where: string,
  severity: FindingSeverity = "error",
): ConformanceFinding {
  return {
    id,
    tool,
    severity,
    expected: EXPECTED_FINDING_IDS[tool].includes(id),
    message,
    where,
  };
}

function capabilityFindings(
  tool: ConformanceTool,
  normalized: NormalizedOutput,
): ConformanceFinding[] {
  const caps = normalized.capabilities;
  const findings: ConformanceFinding[] = [];
  const capOk = (cap: string): boolean => caps[cap]?.status === "ok";

  if (tool === "br" && capOk("br.issues.read")) {
    const items = (normalized.payload as { items?: unknown }).items;
    const page = (normalized.payload as { page?: unknown }).page;
    if (!Array.isArray(items) || !isObject(page)) {
      findings.push(
        finding(
          tool,
          "br.capability.unsatisfied.br.issues.read",
          "br.issues.read requires items[] and page (BeadListResponse shape)",
          "br.md",
        ),
      );
    }
  }
  if (tool === "bv") {
    if (caps["bv.tui"]?.status !== "blocked-by-policy") {
      findings.push(
        finding(tool, "bv.capability.policy.bv.tui", "bv.tui must be blocked-by-policy", "bv.md"),
      );
    }
    if (capOk("bv.robot.triage") && !Array.isArray(normalized.payload.recommendations)) {
      findings.push(
        finding(
          tool,
          "bv.capability.unsatisfied.bv.robot.triage",
          "bv.robot.triage requires recommendations[]",
          "bv.md",
        ),
      );
    }
  }
  if (tool === "ntm" && capOk("ntm.robot.snapshot")) {
    const sessions = normalized.payload.sessions;
    if (!(Array.isArray(sessions) || sessions === null)) {
      findings.push(
        finding(
          tool,
          "ntm.capability.unsatisfied.ntm.robot.snapshot",
          "ntm.robot.snapshot requires sessions[] or null",
          "ntm.md",
        ),
      );
    }
  }
  if (tool === "agent_mail") {
    for (const cap of [
      "agent_mail.messages.read",
      "agent_mail.messages.send",
      "agent_mail.reservations.list",
    ]) {
      if (capOk(cap) && normalized.payload.mcpEvidence !== true) {
        findings.push(
          finding(
            tool,
            `agent_mail.capability.unsatisfied.${cap}`,
            `${cap} is marked ok but fixture only contains CLI help text`,
            "agent_mail.md",
          ),
        );
      }
    }
  }
  if (tool === "git") {
    if (capOk("git.status.read") && !isObject(normalized.payload.branch)) {
      findings.push(
        finding(
          tool,
          "git.capability.unsatisfied.git.status.read",
          "git.status.read requires parsed branch metadata",
          "git.md",
        ),
      );
    }
    if (caps["git.push"]?.status !== "blocked-by-policy") {
      findings.push(
        finding(
          tool,
          "git.capability.policy.git.push",
          "git.push must be blocked-by-policy in fixture mode",
          "git.md",
        ),
      );
    }
  }
  if (tool === "ru" && capOk("ru.schema") && typeof normalized.payload.schemaVersion !== "string") {
    findings.push(
      finding(
        tool,
        "ru.capability.unsatisfied.ru.schema",
        "ru.schema requires schemaVersion",
        "ru.md",
      ),
    );
  }
  if (
    tool === "caam" &&
    capOk("caam.accounts.list") &&
    !Array.isArray(normalized.payload.accounts)
  ) {
    findings.push(
      finding(
        tool,
        "caam.capability.unsatisfied.caam.accounts.list",
        "caam.accounts.list is ok but account-list produced no accounts JSON",
        "caam.md",
      ),
    );
  }
  if (tool === "dcg" && caps["dcg.verdicts.subscribe"]?.status === "ok") {
    findings.push(
      finding(
        tool,
        "dcg.capability.unsatisfied.dcg.verdicts.subscribe",
        "dcg verdict stream cannot be ok from a status/help probe",
        "dcg.md",
      ),
    );
  }
  if (tool === "caut" && !caps["caut.usage.snapshot"]) {
    findings.push(
      finding(
        tool,
        "caut.capability.missing.caut.usage.snapshot",
        "caut.usage.snapshot capability is absent from the stub fixture",
        "caut.md",
      ),
    );
  }
  // hp-8a8: realistic-fixture rules. `ubs.scan` ok requires the help
  // banner in the captured stream so we know the binary actually ran;
  // `jsm.skill.list` ok requires `skills[]` in the JSON output.
  if (tool === "ubs" && capOk("ubs.scan") && normalized.payload.helpBanner !== true) {
    findings.push(
      finding(
        tool,
        "ubs.capability.unsatisfied.ubs.scan",
        "ubs.scan ok requires 'Usage: ubs' banner in stderr/stdout",
        "ubs.md",
      ),
    );
  }
  if (tool === "jsm" && capOk("jsm.skill.list") && !Array.isArray(normalized.payload.skills)) {
    findings.push(
      finding(
        tool,
        "jsm.capability.unsatisfied.jsm.skill.list",
        "jsm.skill.list ok requires skills[] in stdout JSON",
        "jsm.md",
      ),
    );
  }
  return findings;
}

function scenarioCapabilityFindings(tool: ConformanceTool): ConformanceFinding[] {
  const scenariosDir = resolve(fixturesPackageRoot, "scenarios");
  const scenarioNames = [
    "healthy-hour",
    "idle-but-not-stuck",
    "wedged-pane",
    "rate-limited-no-caam",
    "rate-limited-with-caam",
  ];
  const findings: ConformanceFinding[] = [];
  for (const scenario of scenarioNames) {
    const capsPath = resolve(scenariosDir, scenario, "capabilities.json");
    if (!existsSync(capsPath)) continue;
    const allCaps = parseJsonFile(capsPath);
    if (!isObject(allCaps)) continue;
    const toolCaps = allCaps[tool];
    if (!isObject(toolCaps)) continue;
    if (
      tool === "bv" &&
      isObject(toolCaps["bv.tui"]) &&
      (toolCaps["bv.tui"] as CapabilityEntry).status !== "blocked-by-policy"
    ) {
      findings.push(
        finding(
          tool,
          `bv.scenario.${scenario}.tui`,
          `bv.tui is not blocked in ${scenario}`,
          capsPath,
        ),
      );
    }
    if (
      tool === "git" &&
      isObject(toolCaps["git.push"]) &&
      (toolCaps["git.push"] as CapabilityEntry).status !== "blocked-by-policy"
    ) {
      findings.push(
        finding(
          tool,
          `git.scenario.${scenario}.push`,
          `git.push is not blocked in ${scenario}`,
          capsPath,
        ),
      );
    }
  }
  return findings;
}

function assertNegativeCases(tool: ConformanceTool): ConformanceFinding[] {
  const findings: ConformanceFinding[] = [];
  const cases = [
    { name: "empty", text: "", code: "empty_input" },
    { name: "truncated-envelope", text: '{"meta":{"adapter":', code: "invalid_json" },
    { name: "non-object", text: "[]", code: "non_object" },
  ];
  for (const testCase of cases) {
    try {
      parseEnvelopeText(tool, testCase.text, testCase.name);
      findings.push(
        finding(
          tool,
          `${tool}.negative.${testCase.name}`,
          `${testCase.name} unexpectedly parsed`,
          "synthetic negative input",
        ),
      );
    } catch (error) {
      if (!(error instanceof ConformanceParseError) || error.code !== testCase.code) {
        findings.push(
          finding(
            tool,
            `${tool}.negative.${testCase.name}`,
            `${testCase.name} returned wrong error`,
            "synthetic negative input",
          ),
        );
      }
    }
  }
  for (const state of ["malformed-json", "high-volume"] as const) {
    const path = goldenPath(tool, state);
    if (!existsSync(path)) {
      findings.push(
        finding(tool, `${tool}.fixture.missing.${state}`, `missing ${basename(path)}`, path),
      );
      continue;
    }
    try {
      normalizeOutput(tool, loadEnvelope(tool, state), "fixture");
      findings.push(
        finding(tool, `${tool}.negative.${state}`, `${state} unexpectedly parsed`, path),
      );
    } catch (error) {
      if (!(error instanceof ConformanceParseError)) {
        findings.push(
          finding(
            tool,
            `${tool}.negative.${state}`,
            `${state} did not fail with ConformanceParseError`,
            path,
          ),
        );
      }
    }
  }
  return findings;
}

export function runToolConformance(tool: ConformanceTool): ToolConformanceReport {
  const findings: ConformanceFinding[] = [];
  const schemaFile = schemaPath(tool);
  if (!existsSync(schemaFile)) {
    findings.push(
      finding(tool, `${tool}.schema.missing`, `missing schema ${schemaFile}`, schemaFile),
    );
  }

  const fixtureFile = goldenPath(tool, "normal");
  let envelope: GoldenEnvelope;
  let source: "fixture" | "local" = "fixture";
  if (existsSync(fixtureFile)) {
    envelope = loadEnvelope(tool, "normal");
  } else {
    findings.push(
      finding(tool, `${tool}.fixture.missing.normal`, `missing normal fixture`, fixtureFile),
    );
    envelope = {
      meta: {
        adapter: tool,
        state: "normal",
        kind: "stub",
        fixturesVersion: "missing",
        capturedAt: new Date(0).toISOString(),
        source: "missing",
      },
      argv: [tool],
      exit: 127,
      durationMs: 0,
      capabilities: {},
    };
  }

  let normalized: NormalizedOutput | null = null;
  try {
    normalized = normalizeOutput(tool, envelope, source);
  } catch (error) {
    findings.push(
      finding(
        tool,
        `${tool}.normal.parse`,
        error instanceof Error ? error.message : String(error),
        source === "fixture" ? fixtureFile : "local rch status",
      ),
    );
  }

  if (normalized !== null && existsSync(schemaFile)) {
    const schema = parseJsonFile(schemaFile) as JsonSchema;
    const schemaErrors = validateSchema(normalized, schema);
    for (const [index, message] of schemaErrors.entries()) {
      findings.push(finding(tool, `${tool}.schema.${index}`, message, schemaFile));
    }

    const first = canonicalJson(normalized);
    const reparsed = JSON.parse(first) as unknown;
    const second = canonicalJson(reparsed);
    if (first !== second) {
      findings.push(
        finding(
          tool,
          `${tool}.roundtrip.mismatch`,
          "canonical parse/serialize/reparse changed bytes",
          schemaFile,
        ),
      );
    }
    findings.push(...capabilityFindings(tool, normalized));
  }

  findings.push(...scenarioCapabilityFindings(tool));
  findings.push(...assertNegativeCases(tool));

  for (const expectedId of EXPECTED_FINDING_IDS[tool]) {
    if (!findings.some((findingEntry) => findingEntry.id === expectedId)) {
      findings.push(
        finding(
          tool,
          `${tool}.expected-drift-resolved.${expectedId}`,
          `expected drift ${expectedId} was not observed; update the conformance harness and review ledger`,
          "packages/fixtures/conformance/harness.ts",
        ),
      );
    }
  }

  return {
    tool,
    findings,
    schemaPath: schemaFile,
    fixturePath: existsSync(fixtureFile) ? fixtureFile : undefined,
    cases: {
      schema: findings.some((entry) => entry.id.includes(".schema.") && !entry.expected)
        ? "fail"
        : "pass",
      roundTrip: findings.some((entry) => entry.id.includes(".roundtrip.") && !entry.expected)
        ? "fail"
        : "pass",
      negative: findings.some((entry) => entry.id.includes(".negative.") && !entry.expected)
        ? "fail"
        : "pass",
      capabilities: findings.some((entry) => entry.id.includes(".capability.") && !entry.expected)
        ? "fail"
        : "pass",
    },
  };
}

export function runAllConformance(): ConformanceReport {
  const tools = CONFORMANCE_TOOLS.map((tool) => runToolConformance(tool));
  const findings = tools.flatMap((report) => report.findings);
  return {
    tools,
    findings,
    unexpectedFindings: findings.filter((findingEntry) => !findingEntry.expected),
    expectedFindings: findings.filter((findingEntry) => findingEntry.expected),
  };
}

export function assertToolConformance(tool: ConformanceTool): void {
  const report = runToolConformance(tool);
  const unexpected = report.findings.filter((findingEntry) => !findingEntry.expected);
  const expectedIds = [...EXPECTED_FINDING_IDS[tool]].sort();
  const actualExpectedIds = report.findings
    .filter((findingEntry) => findingEntry.expected)
    .map((findingEntry) => findingEntry.id)
    .sort();

  if (unexpected.length > 0 || canonicalJson(expectedIds) !== canonicalJson(actualExpectedIds)) {
    throw new Error(formatToolReport(report));
  }
}

export function formatToolReport(report: ToolConformanceReport): string {
  const lines = [
    `${report.tool}: schema=${report.cases.schema} roundTrip=${report.cases.roundTrip} negative=${report.cases.negative} capabilities=${report.cases.capabilities}`,
  ];
  for (const findingEntry of report.findings) {
    lines.push(
      `- ${findingEntry.expected ? "XFAIL" : "FAIL"} ${findingEntry.id}: ${findingEntry.message} (${findingEntry.where})`,
    );
  }
  return lines.join("\n");
}

export function formatConformanceReport(report: ConformanceReport): string {
  const lines = [
    "# Hoopoe Phase 0 Adapter Conformance",
    "",
    `Tools: ${report.tools.length}`,
    `Unexpected failures: ${report.unexpectedFindings.length}`,
    `Expected drifts: ${report.expectedFindings.length}`,
    "",
    "| Tool | Schema | Round-trip | Negative | Capabilities | Findings |",
    "| --- | --- | --- | --- | --- | --- |",
  ];
  for (const toolReport of report.tools) {
    lines.push(
      `| ${toolReport.tool} | ${toolReport.cases.schema} | ${toolReport.cases.roundTrip} | ${toolReport.cases.negative} | ${toolReport.cases.capabilities} | ${toolReport.findings.length} |`,
    );
  }
  if (report.findings.length > 0) {
    lines.push("", "## Findings");
    for (const findingEntry of report.findings) {
      lines.push(
        `- ${findingEntry.expected ? "XFAIL" : "FAIL"} ${findingEntry.id}: ${findingEntry.message} (${findingEntry.where})`,
      );
    }
  }
  return lines.join("\n");
}
