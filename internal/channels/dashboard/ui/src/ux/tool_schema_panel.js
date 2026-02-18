const SCHEMAS = {
  "fs.edit": {
    required: ["path", "edits"],
    example: {
      path: "src/file.js",
      edits: [{ oldText: "before", newText: "after" }],
    },
  },
  "fs.read": {
    required: ["path"],
    example: { path: "README.md" },
  },
};

export function renderToolSchemaPanel(container) {
  const panel = document.createElement("section");
  panel.className = "panel-section";

  const title = document.createElement("h3");
  title.textContent = "Tool Schema";
  panel.append(title);

  const summary = document.createElement("p");
  summary.className = "muted";
  summary.textContent = "Hardcoded schema preview for Phase 1; backend catalog comes later.";
  panel.append(summary);

  const pre = document.createElement("pre");
  pre.textContent = JSON.stringify(SCHEMAS, null, 2);
  panel.append(pre);

  container.append(panel);
}
