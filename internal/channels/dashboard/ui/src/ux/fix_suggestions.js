const RULES = [
  {
    id: "tool.input_invalid",
    test: (message) => message.includes("missing") || message.includes("invalid"),
    suggestion: "Review tool schema and required fields before retrying.",
  },
  {
    id: "venv.required",
    test: (message) => message.includes("externally-managed-environment"),
    suggestion: "Use a project virtual environment and install packages there.",
  },
  {
    id: "python.module_missing",
    test: (message) => message.includes("modulenotfounderror"),
    suggestion: "Install dependencies inside .venv and run with .venv/bin/python.",
  },
];

export function classifyFixSuggestions(errorLike) {
  const source = typeof errorLike === "string" ? errorLike : JSON.stringify(errorLike || {});
  const normalized = source.toLowerCase();
  return RULES.filter((rule) => rule.test(normalized)).map((rule) => ({ id: rule.id, text: rule.suggestion }));
}

export function renderFixSuggestions(container, errorLike) {
  const suggestions = classifyFixSuggestions(errorLike);
  const section = document.createElement("section");
  section.className = "panel-section";

  const title = document.createElement("h3");
  title.textContent = "Fix Suggestions";
  section.append(title);

  if (!suggestions.length) {
    const empty = document.createElement("p");
    empty.className = "muted";
    empty.textContent = "No suggestions yet. Triggered errors will be classified here.";
    section.append(empty);
    container.append(section);
    return;
  }

  const list = document.createElement("ul");
  for (const suggestion of suggestions) {
    const item = document.createElement("li");
    item.textContent = suggestion.text;
    list.append(item);
  }
  section.append(list);
  container.append(section);
}
