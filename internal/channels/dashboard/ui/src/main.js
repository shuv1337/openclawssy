import { createApiClient } from "./api/client.js";
import { createHashRouter } from "./router/router.js";
import { createStore } from "./state/store.js";
import { chatPage } from "./pages/chat.js";
import { sessionsPage } from "./pages/sessions.js";
import { runsPage } from "./pages/runs.js";
import { schedulerPage } from "./pages/scheduler.js";
import { settingsPage } from "./pages/settings.js";
import { secretsPage } from "./pages/secrets.js";
import { docsPage } from "./pages/docs.js";
import { toolInspector } from "./inspectors/tool_inspector.js";
import { traceInspector } from "./inspectors/trace_inspector.js";
import { createLayout } from "./ui/layout.js";

const ROUTES = [
  { path: "/chat", label: "Chat", page: chatPage },
  { path: "/sessions", label: "Sessions", page: sessionsPage },
  { path: "/runs", label: "Runs", page: runsPage },
  { path: "/scheduler", label: "Scheduler", page: schedulerPage },
  { path: "/settings", label: "Settings", page: settingsPage },
  { path: "/docs", label: "Docs", page: docsPage },
  { path: "/secrets", label: "Secrets", page: secretsPage },
];

const INSPECTORS = [toolInspector, traceInspector];

export function bootDashboardApp() {
  const root = document.getElementById("app");
  if (!root) {
    return;
  }

  document.documentElement.setAttribute("data-dashboard-shell", "phase-1");

  const store = createStore({
    route: "/chat",
    inspectorTab: "tool",
    selectedTrace: null,
    selectedTool: null,
    lastError: null,
  });
  const apiClient = createApiClient();

  const router = createHashRouter({
    routes: ROUTES,
    defaultRoute: "/chat",
    onRouteChange: (route) => store.setState({ route }),
  });

  const layout = createLayout({
    root,
    routes: ROUTES,
    store,
    router,
    apiClient,
    inspectors: INSPECTORS,
  });

  store.subscribe(async (state) => {
    try {
      await layout.render(state);
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error);
      root.innerHTML = `<pre>Dashboard render failed: ${message}</pre>`;
    }
  });

  store.setState({ route: "/chat" });
  router.start();
}
