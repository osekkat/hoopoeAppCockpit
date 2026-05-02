import { existsSync, readdirSync, readFileSync, statSync } from "node:fs";
import path from "node:path";
import ts from "typescript";

const DEFAULT_SCAN_ROOTS = ["package.json", "apps/desktop", "apps/daemon"] as const;

const IGNORED_DIRS = new Set([
  ".git",
  ".turbo",
  "bin",
  "coverage",
  "dist",
  "dist-electron",
  "node_modules",
]);

const TS_LIKE_EXTENSIONS = new Set([
  ".cjs",
  ".cts",
  ".js",
  ".jsx",
  ".mjs",
  ".mts",
  ".ts",
  ".tsx",
]);

const FORBIDDEN_TS_PACKAGES = [
  "openai",
  "@anthropic-ai/sdk",
  "@anthropic-ai/anthropic",
  "@google/generative-ai",
] as const;

const FORBIDDEN_GO_MODULES = [
  "github.com/anthropics/anthropic-sdk-go",
  "github.com/openai/openai-go",
  "github.com/google/generative-ai-go",
] as const;

const FORBIDDEN_PROVIDER_FIELDS = [
  "OPENAI_API_KEY",
  "ANTHROPIC_API_KEY",
  "GEMINI_API_KEY",
] as const;

export type ProviderLintLanguage = "go" | "manifest" | "text" | "ts";

export type ProviderSdkViolation = {
  filePath: string;
  line: number;
  column: number;
  language: ProviderLintLanguage;
  importPath: string;
  message: string;
};

type CandidateFile = {
  filePath: string;
  language: ProviderLintLanguage | "go-mod";
};

function isForbidden(specifier: string, forbidden: readonly string[]) {
  return forbidden.some(
    (blocked) => specifier === blocked || specifier.startsWith(`${blocked}/`),
  );
}

function makeViolation(
  filePath: string,
  language: ProviderLintLanguage,
  importPath: string,
  line: number,
  column: number,
): ProviderSdkViolation {
  return {
    filePath,
    line,
    column,
    language,
    importPath,
    message:
      `Forbidden provider SDK/API-key surface "${importPath}". ` +
      "Hoopoe model access must go through subscription-backed CLIs only; see plan.md §13 and Appendix C #11.",
  };
}

function locationForOffset(sourceFile: ts.SourceFile, offset: number) {
  const location = sourceFile.getLineAndCharacterOfPosition(offset);
  return { line: location.line + 1, column: location.character + 1 };
}

function collectForbiddenFieldMentions(
  filePath: string,
  text: string,
): ProviderSdkViolation[] {
  const violations: ProviderSdkViolation[] = [];
  const lines = text.split(/\r?\n/);

  lines.forEach((lineText, lineIndex) => {
    for (const fieldName of FORBIDDEN_PROVIDER_FIELDS) {
      const column = lineText.indexOf(fieldName);
      if (column >= 0) {
        violations.push(
          makeViolation(filePath, "text", fieldName, lineIndex + 1, column + 1),
        );
      }
    }
  });

  return violations;
}

export function collectTypeScriptViolations(
  filePath: string,
  text: string,
): ProviderSdkViolation[] {
  const sourceFile = ts.createSourceFile(
    filePath,
    text,
    ts.ScriptTarget.Latest,
    true,
    ts.ScriptKind.TSX,
  );
  const violations: ProviderSdkViolation[] = [];

  function addSpecifier(specifier: ts.StringLiteralLike) {
    if (!isForbidden(specifier.text, FORBIDDEN_TS_PACKAGES)) {
      return;
    }

    const { line, column } = locationForOffset(sourceFile, specifier.getStart(sourceFile));
    violations.push(makeViolation(filePath, "ts", specifier.text, line, column));
  }

  function visit(node: ts.Node): void {
    if (
      (ts.isImportDeclaration(node) || ts.isExportDeclaration(node)) &&
      node.moduleSpecifier &&
      ts.isStringLiteralLike(node.moduleSpecifier)
    ) {
      addSpecifier(node.moduleSpecifier);
    }

    if (
      ts.isImportTypeNode(node) &&
      ts.isLiteralTypeNode(node.argument) &&
      ts.isStringLiteralLike(node.argument.literal)
    ) {
      addSpecifier(node.argument.literal);
    }

    if (
      ts.isCallExpression(node) &&
      node.arguments.length === 1 &&
      ts.isStringLiteralLike(node.arguments[0]) &&
      (node.expression.kind === ts.SyntaxKind.ImportKeyword ||
        (ts.isIdentifier(node.expression) && node.expression.text === "require"))
    ) {
      addSpecifier(node.arguments[0]);
    }

    ts.forEachChild(node, visit);
  }

  visit(sourceFile);
  violations.push(...collectForbiddenFieldMentions(filePath, text));
  return violations;
}

export function collectGoViolations(
  filePath: string,
  text: string,
): ProviderSdkViolation[] {
  const violations: ProviderSdkViolation[] = [];
  const lines = text.split(/\r?\n/);
  let inImportBlock = false;

  lines.forEach((lineText, lineIndex) => {
    const trimmed = lineText.trim();

    if (trimmed.startsWith("import (")) {
      inImportBlock = true;
      return;
    }

    if (inImportBlock && trimmed === ")") {
      inImportBlock = false;
      return;
    }

    const importMatch = inImportBlock
      ? trimmed.match(/^(?:[\w_.]+\s+)?"([^"]+)"/)
      : trimmed.match(/^import\s+(?:[\w_.]+\s+)?"([^"]+)"/);

    if (!importMatch?.[1] || !isForbidden(importMatch[1], FORBIDDEN_GO_MODULES)) {
      return;
    }

    violations.push(
      makeViolation(
        filePath,
        "go",
        importMatch[1],
        lineIndex + 1,
        lineText.indexOf(importMatch[1]) + 1,
      ),
    );
  });

  violations.push(...collectForbiddenFieldMentions(filePath, text));
  return violations;
}

export function collectPackageManifestViolations(
  filePath: string,
  text: string,
): ProviderSdkViolation[] {
  const violations = collectForbiddenFieldMentions(filePath, text);
  let manifest: unknown;

  try {
    manifest = JSON.parse(text);
  } catch {
    return violations;
  }

  if (!manifest || typeof manifest !== "object") {
    return violations;
  }

  const dependencySections = [
    "dependencies",
    "devDependencies",
    "optionalDependencies",
    "peerDependencies",
  ];

  for (const sectionName of dependencySections) {
    const section = (manifest as Record<string, unknown>)[sectionName];
    if (!section || typeof section !== "object") {
      continue;
    }

    for (const packageName of Object.keys(section)) {
      if (!isForbidden(packageName, FORBIDDEN_TS_PACKAGES)) {
        continue;
      }

      const lineIndex = text.split(/\r?\n/).findIndex((lineText) =>
        lineText.includes(`"${packageName}"`),
      );
      violations.push(
        makeViolation(
          filePath,
          "manifest",
          packageName,
          Math.max(lineIndex + 1, 1),
          lineIndex >= 0 ? text.split(/\r?\n/)[lineIndex]!.indexOf(packageName) + 1 : 1,
        ),
      );
    }
  }

  return violations;
}

export function collectGoModViolations(
  filePath: string,
  text: string,
): ProviderSdkViolation[] {
  const violations = collectForbiddenFieldMentions(filePath, text);
  const lines = text.split(/\r?\n/);

  lines.forEach((lineText, lineIndex) => {
    for (const modulePath of FORBIDDEN_GO_MODULES) {
      const column = lineText.indexOf(modulePath);
      if (column >= 0) {
        violations.push(
          makeViolation(filePath, "manifest", modulePath, lineIndex + 1, column + 1),
        );
      }
    }
  });

  return violations;
}

function candidateLanguage(filePath: string): CandidateFile["language"] | null {
  const basename = path.basename(filePath);
  const extension = path.extname(filePath);

  if (basename === "package.json") {
    return "manifest";
  }

  if (basename === "go.mod") {
    return "go-mod";
  }

  if (extension === ".go") {
    return "go";
  }

  if (TS_LIKE_EXTENSIONS.has(extension)) {
    return "ts";
  }

  return null;
}

export function listCandidateFiles(
  cwd = process.cwd(),
  roots: readonly string[] = DEFAULT_SCAN_ROOTS,
): CandidateFile[] {
  const files: CandidateFile[] = [];

  function walk(currentPath: string) {
    const stat = statSync(currentPath);

    if (stat.isDirectory()) {
      if (IGNORED_DIRS.has(path.basename(currentPath))) {
        return;
      }

      for (const entry of readdirSync(currentPath)) {
        walk(path.join(currentPath, entry));
      }
      return;
    }

    if (!stat.isFile()) {
      return;
    }

    const language = candidateLanguage(currentPath);
    if (language) {
      files.push({ filePath: currentPath, language });
    }
  }

  for (const root of roots) {
    const absoluteRoot = path.resolve(cwd, root);
    if (existsSync(absoluteRoot)) {
      walk(absoluteRoot);
    }
  }

  return files.toSorted((a, b) => a.filePath.localeCompare(b.filePath));
}

export function collectViolationsForFile(
  candidate: CandidateFile,
): ProviderSdkViolation[] {
  const text = readFileSync(candidate.filePath, "utf8");

  if (candidate.language === "go") {
    return collectGoViolations(candidate.filePath, text);
  }

  if (candidate.language === "go-mod") {
    return collectGoModViolations(candidate.filePath, text);
  }

  if (candidate.language === "manifest") {
    return collectPackageManifestViolations(candidate.filePath, text);
  }

  return collectTypeScriptViolations(candidate.filePath, text);
}

export function scanWorkspace(
  cwd = process.cwd(),
  roots: readonly string[] = DEFAULT_SCAN_ROOTS,
): ProviderSdkViolation[] {
  return listCandidateFiles(cwd, roots).flatMap(collectViolationsForFile);
}

export function formatViolation(violation: ProviderSdkViolation) {
  return `${path.relative(process.cwd(), violation.filePath)}:${violation.line}:${violation.column} - ${violation.message}`;
}

if (import.meta.main) {
  const violations = scanWorkspace();

  if (violations.length > 0) {
    console.error("Provider SDK lint failed:");
    for (const violation of violations) {
      console.error(`  ${formatViolation(violation)}`);
    }
    process.exit(1);
  }
}
