// `@hoopoe/slo` — percentile computation (hp-5ja).
//
// Uses linear interpolation between adjacent ranks (the same as
// numpy's default `interpolation='linear'`); for samples sorted in
// ascending order and a target percentile p in [0, 100]:
//
//   rank      = (p / 100) * (n - 1)
//   lower     = floor(rank)
//   upper     = ceil(rank)
//   fraction  = rank - lower
//   pValue    = samples[lower] + (samples[upper] - samples[lower]) * fraction
//
// Edge cases:
//   - empty input throws
//   - n === 1 returns the single sample for any percentile
//   - non-finite samples are filtered before computation
//   - p outside (0, 100] throws

export class PercentileError extends Error {
  override readonly name = "PercentileError";
  constructor(message: string) {
    super(message);
  }
}

export function percentile(samples: readonly number[], p: number): number {
  if (!Number.isFinite(p) || p <= 0 || p > 100) {
    throw new PercentileError(`percentile must be in (0, 100], got ${p}`);
  }
  const finite: number[] = [];
  for (const sample of samples) {
    if (Number.isFinite(sample)) finite.push(sample);
  }
  if (finite.length === 0) {
    throw new PercentileError("cannot compute percentile of empty sample set");
  }
  if (finite.length === 1) return finite[0] as number;
  const sorted = finite.slice().sort((a, b) => a - b);
  const rank = (p / 100) * (sorted.length - 1);
  const lower = Math.floor(rank);
  const upper = Math.ceil(rank);
  if (lower === upper) return sorted[lower] as number;
  const fraction = rank - lower;
  const lowerValue = sorted[lower] as number;
  const upperValue = sorted[upper] as number;
  return lowerValue + (upperValue - lowerValue) * fraction;
}
