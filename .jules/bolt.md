## 2024-05-22 - Scheduler Loop Optimization
**Learning:** Even with small N (100 jobs), sorting and `MarshalIndent` in the hot path of a scheduler loop caused significant overhead (~21% of total execution time including I/O). `json.Marshal` is measurably faster than `MarshalIndent`, and avoiding sorting in the check loop reduced CPU time by ~40%.
**Action:** Avoid sorting in `List` methods used by periodic checkers. Prefer `json.Marshal` for internal state files unless human readability is critical.

## 2024-05-23 - Scheduler Tick Optimization
**Learning:** In a high-frequency loop (every 1ms or 1s), separate method calls for state checks (e.g., `IsPaused` then `List`) that each perform file system stats and lock acquisition add measurable overhead. Combining these into a single `tickSnapshot` method reduced execution time by ~5.5% in benchmarks by halving the number of `stat` calls and lock acquisitions per tick.
**Action:** Combine related state checks and data retrieval into single, atomic methods for tight loops to minimize I/O and lock contention.
