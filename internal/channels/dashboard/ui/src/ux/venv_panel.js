export function renderVenvPanel(container) {
  const panel = document.createElement("section");
  panel.className = "panel-section";

  const title = document.createElement("h3");
  title.textContent = "Python Env";

  const description = document.createElement("p");
  description.className = "muted";
  description.textContent = "UI-only helper for common virtual environment commands.";

  const commands = [
    "python3 -m venv .venv",
    ".venv/bin/pip install -r requirements.txt",
    ".venv/bin/python your_script.py",
  ];

  const list = document.createElement("ul");
  for (const command of commands) {
    const item = document.createElement("li");
    const code = document.createElement("code");
    code.textContent = command;
    item.append(code);
    list.append(item);
  }

  panel.append(title, description, list);
  container.append(panel);
}
