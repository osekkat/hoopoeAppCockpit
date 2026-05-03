#!/usr/bin/env bun
import { formatConformanceReport, runAllConformance } from "../../packages/fixtures/conformance/harness.ts";

const report = runAllConformance();
const allowKnownDrift = process.argv.includes("--allow-known-drift");

console.log(formatConformanceReport(report));

if (report.unexpectedFindings.length > 0) {
  process.exit(1);
}

if (!allowKnownDrift && report.expectedFindings.length > 0) {
  process.exit(2);
}
