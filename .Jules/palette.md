## 2025-10-26 - Reusing Scheduler Pills for General Status Badges
**Learning:** The `scheduler-status-pill` class in `styles.css` provides a solid foundation for status badges (pill shape, colors).
**Action:** Created a generic `.status-badge` class that mirrors `scheduler-status-pill` but with clearer modifier names (`running`, `completed`, `failed`, `queued`) for use across the application, starting with the Runs list.

## 2026-02-18 - Keyboard Support for Resizers
**Learning:** Splitter/resizer elements (`role="separator"`) often lack keyboard accessibility for toggling visibility. While Arrow keys allow resizing, users expect `Enter` to perform a default action (like collapsing).
**Action:** Implemented `Enter` key support on resizers to toggle sidebar visibility, mirroring the double-click behavior for mouse users.
