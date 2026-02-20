## 2024-05-22 - Scheduler Loop Optimization
**Learning:** Even with small N (100 jobs), sorting and `MarshalIndent` in the hot path of a scheduler loop caused significant overhead (~21% of total execution time including I/O). `json.Marshal` is measurably faster than `MarshalIndent`, and avoiding sorting in the check loop reduced CPU time by ~40%.
**Action:** Avoid sorting in `List` methods used by periodic checkers. Prefer `json.Marshal` for internal state files unless human readability is critical.
