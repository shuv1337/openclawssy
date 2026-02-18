function safeStringify(value) {
  try {
    return JSON.stringify(value, null, 2);
  } catch (_err) {
    return String(value);
  }
}

export function renderJSONViewer(container, value, options = {}) {
  const { title = "JSON", maxChars = 4000 } = options;
  container.classList.add("json-viewer");

  const label = document.createElement("h3");
  label.textContent = title;
  label.className = "json-viewer-title";

  const pre = document.createElement("pre");
  const text = safeStringify(value);
  if (text.length > maxChars) {
    pre.textContent = `${text.slice(0, maxChars)}\n...truncated...`;
    pre.dataset.full = text;

    const button = document.createElement("button");
    button.type = "button";
    button.className = "button-link";
    button.textContent = "Expand";
    button.addEventListener("click", () => {
      pre.textContent = pre.dataset.full || text;
      button.remove();
    });
    container.append(label, button, pre);
    return;
  }

  pre.textContent = text;
  container.append(label, pre);
}
