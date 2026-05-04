export const PLANNING_PROMPTS_SCHEMA_VERSION = 1 as const;

export interface PromptFrontmatter {
  id: string;
  version: number;
  hash: string;
  owner: string;
  lastEdited: string;
  appliesToPipelineVersions: readonly string[];
}

export interface PlanningPrompt {
  frontmatter: PromptFrontmatter;
  body: string;
  sourcePath: string;
}

export interface PromptManifestEntry {
  id: string;
  version: number;
  path: string;
  hash: string;
  owner: string;
  appliesToPipelineVersions: readonly string[];
}

export interface PromptManifest {
  schemaVersion: typeof PLANNING_PROMPTS_SCHEMA_VERSION;
  prompts: readonly PromptManifestEntry[];
  sourcePath: string;
}

export interface RegressionFixture {
  promptId: string;
  pipelineVersion: string;
  inputShape: Record<string, unknown>;
  expectedOutputSchema: Record<string, unknown>;
  sampleAcceptableOutputs: readonly string[];
}

export interface PromptRegressionResult {
  promptId: string;
  fixturePath: string;
  ok: boolean;
  diagnostics: readonly string[];
}
