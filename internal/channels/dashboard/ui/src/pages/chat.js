const CHAT_DEFAULTS = {
  userID: "dashboard_user",
  roomID: "dashboard",
  agentID: "default",
};

const RUN_POLL_MS = 1500;
const SESSION_POLL_MS = 2000;
const SESSION_MESSAGES_LIMIT = 200;
const TERMINAL_RUN_STATUSES = new Set(["completed", "failed", "canceled"]);

const chatViewState = {
  draft: "",
  transcript: [],
  sendPending: false,
  sendError: null,

  currentRunID: "",
  currentRunStatus: "idle",
  currentRunStartedAtMs: 0,
  currentSessionID: "",
  currentRunLastUpdatedAt: "",
  currentRunLastOutput: "",

  latestToolActivity: null,
  lastErrorSummary: "",
  loopRisk: {
    level: "low",
    score: 0,
    reasons: [],
    repeatCount: 0,
    failureCount: 0,
    windowSize: 0,
  },

  polling: false,
  pollToken: 0,
  runPollTimer: 0,
  sessionPollTimer: 0,
  runPollInFlight: false,
  sessionPollInFlight: false,

  transcriptPinned: true,
  transcriptScrollTop: 0,

  container: null,
  apiClient: null,
  store: null,
};

function safeText(value) {
  return String(value || "").trim();
}

function formatDateTime(value) {
  if (!value) {
    return "-";
  }
  const parsed = new Date(value);
  if (Number.isNaN(parsed.getTime())) {
    return "-";
  }
  return parsed.toLocaleString();
}

function compactText(value, maxChars = 220) {
  const text = safeText(value).replace(/\s+/g, " ");
  if (!text) {
    return "";
  }
  if (text.length <= maxChars) {
    return text;
  }
  return `${text.slice(0, Math.max(0, maxChars - 3))}...`;
}

function parseMaybeJSON(value) {
  if (value === null || value === undefined) {
    return null;
  }
  if (typeof value === "object") {
    return value;
  }
  const text = safeText(value);
  if (!text) {
    return null;
  }
  try {
    return JSON.parse(text);
  } catch (_error) {
    return null;
  }
}

function asDisplayText(value) {
  if (value === null || value === undefined) {
    return "";
  }
  if (typeof value === "string") {
    return value;
  }
  try {
    return JSON.stringify(value, null, 2);
  } catch (_error) {
    return String(value);
  }
}

function firstNonEmpty(...values) {
  for (const value of values) {
    const text = safeText(value);
    if (text) {
      return text;
    }
  }
  return "";
}

function isTerminalStatus(status) {
  return TERMINAL_RUN_STATUSES.has(safeText(status).toLowerCase());
}

function isTranscriptNearBottom(element, threshold = 36) {
  if (!element) {
    return true;
  }
  const distance = element.scrollHeight - (element.scrollTop + element.clientHeight);
  return distance <= threshold;
}

function toErrorPayload(scope, err, extra = {}) {
  if (!err) {
    return { scope, message: "Unknown error", ...extra };
  }
  return {
    scope,
    message: err.message || String(err),
    code: err.code || "error.unknown",
    status: Number(err.status) || 0,
    details: err.details || null,
    ...extra,
  };
}

function updateLastError(store, errorLike) {
  if (!store) {
    return;
  }
  const current = JSON.stringify(store.getState().lastError || null);
  const next = JSON.stringify(errorLike || null);
  if (current === next) {
    return;
  }
  store.setState({ lastError: errorLike || null });
}

function pollSessionMessagesPath(sessionID) {
  return `/api/admin/chat/sessions/${encodeURIComponent(sessionID)}/messages?limit=${encodeURIComponent(String(SESSION_MESSAGES_LIMIT))}`;
}

function toToolEvent(message, index) {
  const parsed = parseMaybeJSON(message?.content);
  const toolName = firstNonEmpty(message?.tool_name, parsed?.tool, parsed?.name, "unknown.tool");
  const toolCallID = firstNonEmpty(message?.tool_call_id, parsed?.id, parsed?.tool_call_id);
  const runID = firstNonEmpty(message?.run_id, parsed?.run_id);
  const summary = firstNonEmpty(parsed?.summary, parsed?.message);
  const argsText = asDisplayText(parsed?.arguments ?? parsed?.args ?? parsed?.input ?? parsed?.params);
  const outputText = asDisplayText(parsed?.output ?? parsed?.result ?? parsed?.response);
  const errorText = asDisplayText(parsed?.error ?? parsed?.callback_error ?? parsed?.stderr);
  const status = safeText(errorText) ? "failed" : "ok";

  return {
    tool: toolName,
    toolCallID,
    runID,
    summary,
    argsText,
    outputText,
    errorText,
    status,
    ts: String(message?.ts || ""),
    index,
  };
}

function normalizeToolEvents(messages) {
  if (!Array.isArray(messages)) {
    return [];
  }
  return messages
    .map((message, index) => {
      if (!message || typeof message !== "object") {
        return null;
      }
      const role = safeText(message.role).toLowerCase();
      if (role !== "tool") {
        return null;
      }
      return toToolEvent(message, index);
    })
    .filter((item) => item !== null);
}

function buildLoopRisk(toolEvents) {
  const window = Array.isArray(toolEvents) ? toolEvents.slice(-8) : [];
  const failures = window.filter((event) => event.status === "failed");
  const repeatCounts = new Map();
  const failureSignatures = new Map();

  window.forEach((event) => {
    repeatCounts.set(event.tool, (repeatCounts.get(event.tool) || 0) + 1);
    if (event.status === "failed") {
      const signature = `${event.tool}|${compactText(event.errorText, 180)}`;
      failureSignatures.set(signature, (failureSignatures.get(signature) || 0) + 1);
    }
  });

  let repeatedToolName = "";
  let repeatedToolCount = 0;
  repeatCounts.forEach((count, toolName) => {
    if (count > repeatedToolCount) {
      repeatedToolCount = count;
      repeatedToolName = toolName;
    }
  });

  let repeatedFailureCount = 0;
  failureSignatures.forEach((count) => {
    if (count > repeatedFailureCount) {
      repeatedFailureCount = count;
    }
  });

  let score = 0;
  const reasons = [];

  if (repeatedToolCount >= 3) {
    score += 2;
    reasons.push(`${repeatedToolName || "tool"} repeated ${repeatedToolCount}x in the recent window.`);
  }
  if (repeatedToolCount >= 5) {
    score += 2;
  }
  if (failures.length >= 3) {
    score += 2;
    reasons.push(`${failures.length} recent tool failures detected.`);
  }
  if (window.length >= 4 && failures.length / window.length >= 0.5) {
    score += 1;
  }
  if (repeatedFailureCount >= 2) {
    score += 2;
    reasons.push("Same tool error repeated across recent attempts.");
  }
  if (repeatedFailureCount >= 3) {
    score += 1;
  }

  const level = score >= 6 ? "high" : score >= 3 ? "medium" : "low";
  return {
    level,
    score,
    reasons,
    repeatCount: repeatedToolCount,
    failureCount: failures.length,
    windowSize: window.length,
  };
}

function replacePendingAssistant(message) {
  for (let index = chatViewState.transcript.length - 1; index >= 0; index -= 1) {
    const item = chatViewState.transcript[index];
    if (item?.role === "assistant" && item?.pending) {
      chatViewState.transcript[index] = {
        role: "assistant",
        content: message,
        pending: false,
        ts: new Date().toISOString(),
      };
      return;
    }
  }
  chatViewState.transcript.push({ role: "assistant", content: message, pending: false, ts: new Date().toISOString() });
}

function updatePendingAssistant(message) {
  const text = safeText(message);
  if (!text) {
    return;
  }
  for (let index = chatViewState.transcript.length - 1; index >= 0; index -= 1) {
    const item = chatViewState.transcript[index];
    if (item?.role === "assistant" && item?.pending) {
      chatViewState.transcript[index] = {
        role: "assistant",
        content: text,
        pending: true,
        ts: item.ts || new Date().toISOString(),
      };
      return;
    }
  }
}

function pushUserAndPendingAssistant(message) {
  const now = new Date().toISOString();
  chatViewState.transcript.push({ role: "user", content: message, pending: false, ts: now });
  chatViewState.transcript.push({ role: "assistant", content: "Thinking...", pending: true, ts: now });
}

function clearTimers() {
  if (chatViewState.runPollTimer) {
    window.clearTimeout(chatViewState.runPollTimer);
    chatViewState.runPollTimer = 0;
  }
  if (chatViewState.sessionPollTimer) {
    window.clearTimeout(chatViewState.sessionPollTimer);
    chatViewState.sessionPollTimer = 0;
  }
}

function stopPolling() {
  chatViewState.polling = false;
  chatViewState.pollToken += 1;
  chatViewState.runPollInFlight = false;
  chatViewState.sessionPollInFlight = false;
  clearTimers();
}

function isChatRouteActive() {
  if (!chatViewState.store) {
    return false;
  }
  return chatViewState.store.getState().route === "/chat";
}

function rerenderIfActive() {
  const container = chatViewState.container;
  if (!container || !container.isConnected || !isChatRouteActive()) {
    return;
  }
  renderChatPage();
}

function scheduleRunPoll(token, immediate = false) {
  if (!chatViewState.polling || token !== chatViewState.pollToken) {
    return;
  }
  if (chatViewState.runPollTimer) {
    window.clearTimeout(chatViewState.runPollTimer);
  }
  chatViewState.runPollTimer = window.setTimeout(() => {
    void pollRunOnce(token);
  }, immediate ? 0 : RUN_POLL_MS);
}

function scheduleSessionPoll(token, immediate = false) {
  if (!chatViewState.polling || token !== chatViewState.pollToken) {
    return;
  }
  if (!safeText(chatViewState.currentSessionID)) {
    return;
  }
  if (chatViewState.sessionPollTimer) {
    window.clearTimeout(chatViewState.sessionPollTimer);
  }
  chatViewState.sessionPollTimer = window.setTimeout(() => {
    void pollSessionMessagesOnce(token);
  }, immediate ? 0 : SESSION_POLL_MS);
}

function startPolling({ runID, sessionID }) {
  const nextRunID = safeText(runID);
  const nextSessionID = safeText(sessionID);
  if (!nextRunID) {
    return;
  }

  const sameRun = chatViewState.polling && chatViewState.currentRunID === nextRunID;
  chatViewState.currentRunID = nextRunID;
  if (nextSessionID) {
    chatViewState.currentSessionID = nextSessionID;
  }
  chatViewState.currentRunStartedAtMs = Date.now();

  if (sameRun) {
    if (nextSessionID) {
      scheduleSessionPoll(chatViewState.pollToken, true);
    }
    return;
  }

  stopPolling();
  chatViewState.currentRunID = nextRunID;
  if (nextSessionID) {
    chatViewState.currentSessionID = nextSessionID;
  }
  chatViewState.currentRunStatus = "running";
  chatViewState.currentRunStartedAtMs = Date.now();
  chatViewState.polling = true;
  chatViewState.pollToken += 1;

  const token = chatViewState.pollToken;
  scheduleRunPoll(token, true);
  if (chatViewState.currentSessionID) {
    scheduleSessionPoll(token, true);
  }
}

async function pollRunOnce(token) {
  if (!isChatRouteActive()) {
    stopPolling();
    return;
  }
  if (!chatViewState.polling || token !== chatViewState.pollToken) {
    return;
  }
  if (chatViewState.runPollInFlight) {
    scheduleRunPoll(token, false);
    return;
  }
  const runID = safeText(chatViewState.currentRunID);
  if (!runID) {
    stopPolling();
    return;
  }

  chatViewState.runPollInFlight = true;
  try {
    const run = await chatViewState.apiClient.get(`/v1/runs/${encodeURIComponent(runID)}`);
    const status = safeText(run?.status).toLowerCase() || "unknown";
    chatViewState.currentRunStatus = status;
    chatViewState.currentRunLastUpdatedAt = String(run?.updated_at || "");
    chatViewState.currentRunLastOutput = String(run?.output || "");

    const discoveredSessionID = safeText(run?.session_id);
    if (discoveredSessionID && discoveredSessionID !== chatViewState.currentSessionID) {
      chatViewState.currentSessionID = discoveredSessionID;
      scheduleSessionPoll(token, true);
    }

    if (safeText(run?.error)) {
      chatViewState.lastErrorSummary = compactText(run.error, 280);
      updateLastError(
        chatViewState.store,
        toErrorPayload("chat.run", { message: run.error }, { run_id: runID, status: run?.status || "" })
      );
    }

    if (isTerminalStatus(status)) {
      if (status === "completed") {
        replacePendingAssistant(safeText(run?.output) || "(completed with no output)");
      } else if (status === "failed") {
        const message = safeText(run?.error) || "Run failed.";
        replacePendingAssistant(`Error: ${message}`);
        chatViewState.lastErrorSummary = compactText(message, 280);
      } else if (status === "canceled") {
        replacePendingAssistant("Run canceled.");
      }
      stopPolling();
      rerenderIfActive();
      return;
    }

    const elapsedSeconds =
      chatViewState.currentRunStartedAtMs > 0
        ? Math.max(1, Math.floor((Date.now() - chatViewState.currentRunStartedAtMs) / 1000))
        : 0;
    let progress = `Still working on run ${runID} (status: ${status}`;
    if (elapsedSeconds > 0) {
      progress += `, ${elapsedSeconds}s elapsed`;
    }
    progress += ").";
    const latest = chatViewState.latestToolActivity;
    if (latest) {
      const detail = compactText(firstNonEmpty(latest.summary, latest.errorText, latest.outputText, latest.argsText), 180);
      if (detail) {
        progress += ` Latest tool activity: ${latest.tool} - ${detail}`;
      } else if (latest.tool) {
        progress += ` Latest tool activity: ${latest.tool}.`;
      }
    }
    updatePendingAssistant(progress);
  } catch (err) {
    chatViewState.lastErrorSummary = compactText(err?.message || String(err), 280);
    updateLastError(chatViewState.store, toErrorPayload("chat.run_poll", err, { run_id: chatViewState.currentRunID }));
  } finally {
    chatViewState.runPollInFlight = false;
  }

  rerenderIfActive();
  scheduleRunPoll(token, false);
}

async function pollSessionMessagesOnce(token) {
  if (!isChatRouteActive()) {
    stopPolling();
    return;
  }
  if (!chatViewState.polling || token !== chatViewState.pollToken) {
    return;
  }
  if (chatViewState.sessionPollInFlight) {
    scheduleSessionPoll(token, false);
    return;
  }
  const sessionID = safeText(chatViewState.currentSessionID);
  if (!sessionID) {
    return;
  }

  chatViewState.sessionPollInFlight = true;
  try {
    const payload = await chatViewState.apiClient.get(pollSessionMessagesPath(sessionID));
    const messages = Array.isArray(payload?.messages) ? payload.messages : [];
    const toolEvents = normalizeToolEvents(messages);
    const latest = toolEvents.length ? toolEvents[toolEvents.length - 1] : null;
    chatViewState.latestToolActivity = latest;
    chatViewState.loopRisk = buildLoopRisk(toolEvents);

    const latestFailed = toolEvents
      .slice()
      .reverse()
      .find((event) => event.status === "failed" && safeText(event.errorText));
    if (latestFailed) {
      chatViewState.lastErrorSummary = compactText(latestFailed.errorText, 280);
      updateLastError(
        chatViewState.store,
        toErrorPayload(
          "chat.tool_activity",
          { message: latestFailed.errorText },
          {
            run_id: latestFailed.runID || chatViewState.currentRunID,
            session_id: sessionID,
            tool: latestFailed.tool,
          }
        )
      );
    }
  } catch (err) {
    chatViewState.lastErrorSummary = compactText(err?.message || String(err), 280);
    updateLastError(
      chatViewState.store,
      toErrorPayload("chat.session_poll", err, { session_id: sessionID, run_id: chatViewState.currentRunID })
    );
  } finally {
    chatViewState.sessionPollInFlight = false;
  }

  rerenderIfActive();
  scheduleSessionPoll(token, false);
}

async function sendMessage() {
  if (chatViewState.sendPending) {
    return;
  }
  const message = safeText(chatViewState.draft);
  if (!message) {
    return;
  }

  chatViewState.sendPending = true;
  chatViewState.sendError = null;
  pushUserAndPendingAssistant(message);
  chatViewState.draft = "";
  rerenderIfActive();

  try {
    const payload = await chatViewState.apiClient.post("/v1/chat/messages", {
      user_id: CHAT_DEFAULTS.userID,
      room_id: CHAT_DEFAULTS.roomID,
      agent_id: CHAT_DEFAULTS.agentID,
      message,
    });

    const runID = safeText(payload?.id);
    const sessionID = safeText(payload?.session_id);
    if (sessionID) {
      chatViewState.currentSessionID = sessionID;
    }

    if (runID) {
      chatViewState.currentRunID = runID;
      chatViewState.currentRunStatus = safeText(payload?.status).toLowerCase() || "queued";
      updatePendingAssistant(
        safeText(payload?.response) ||
          `Working on it now. Run ${runID} is ${chatViewState.currentRunStatus || "queued"}. I will follow up when it completes or fails.`
      );
      startPolling({ runID, sessionID });
    } else {
      const directResponse = safeText(payload?.response);
      replacePendingAssistant(directResponse || "Request accepted.");
      chatViewState.currentRunStatus = "idle";
      chatViewState.currentRunStartedAtMs = 0;
    }
  } catch (err) {
    const messageText = err?.message || String(err);
    chatViewState.sendError = messageText;
    chatViewState.lastErrorSummary = compactText(messageText, 280);
    replacePendingAssistant(`Error: ${messageText}`);
    updateLastError(chatViewState.store, toErrorPayload("chat.send", err));
  } finally {
    chatViewState.sendPending = false;
    rerenderIfActive();
  }
}

function renderChatPage() {
  const container = chatViewState.container;
  if (!container) {
    return;
  }
  const previousTranscript = container.querySelector(".chat-transcript");
  if (previousTranscript) {
    chatViewState.transcriptPinned = isTranscriptNearBottom(previousTranscript);
    chatViewState.transcriptScrollTop = previousTranscript.scrollTop;
  }
  container.innerHTML = "";

  const heading = document.createElement("h2");
  heading.textContent = "Chat";

  const note = document.createElement("p");
  note.className = "muted";
  note.textContent = "Send prompts from dashboard defaults and watch live run/tool activity while the agent is in-flight.";

  const page = document.createElement("section");
  page.className = "chat-page";

  const transcriptPane = document.createElement("section");
  transcriptPane.className = "chat-transcript-pane";
  const transcriptTitle = document.createElement("h3");
  transcriptTitle.textContent = "Transcript";
  const transcript = document.createElement("div");
  transcript.className = "chat-transcript";
  transcript.addEventListener("scroll", () => {
    chatViewState.transcriptPinned = isTranscriptNearBottom(transcript);
    chatViewState.transcriptScrollTop = transcript.scrollTop;
  });

  if (!chatViewState.transcript.length) {
    const empty = document.createElement("p");
    empty.className = "muted";
    empty.textContent = "No messages yet. Send a prompt to start.";
    transcript.append(empty);
  } else {
    chatViewState.transcript.forEach((item) => {
      if (!item || (item.role !== "user" && item.role !== "assistant")) {
        return;
      }
      const message = document.createElement("article");
      message.className = `chat-message chat-${item.role}`;
      if (item.pending) {
        message.classList.add("pending");
      }

      const meta = document.createElement("p");
      meta.className = "chat-message-meta";
      meta.textContent = `${item.role} · ${formatDateTime(item.ts)}`;

      const body = document.createElement("pre");
      body.className = "chat-message-content";
      body.textContent = String(item.content || "");

      message.append(meta, body);
      transcript.append(message);
    });
  }

  const composer = document.createElement("form");
  composer.className = "chat-composer";
  composer.addEventListener("submit", (event) => {
    event.preventDefault();
    void sendMessage();
  });

  const input = document.createElement("textarea");
  input.className = "chat-input";
  input.placeholder = "Ask the agent to investigate, summarize, or run a workflow...";
  input.value = chatViewState.draft;
  input.rows = 3;
  input.addEventListener("input", () => {
    chatViewState.draft = input.value;
  });

  const composerActions = document.createElement("div");
  composerActions.className = "chat-composer-actions";
  const defaultsMeta = document.createElement("span");
  defaultsMeta.className = "muted";
  defaultsMeta.textContent = `Defaults: ${CHAT_DEFAULTS.userID} / ${CHAT_DEFAULTS.roomID} / ${CHAT_DEFAULTS.agentID}`;

  const sendButton = document.createElement("button");
  sendButton.type = "submit";
  sendButton.className = "chat-send-button";
  sendButton.textContent = chatViewState.sendPending ? "Sending..." : "Send";
  sendButton.disabled = chatViewState.sendPending;
  composerActions.append(defaultsMeta, sendButton);

  if (chatViewState.sendError) {
    const sendError = document.createElement("p");
    sendError.className = "chat-send-error";
    sendError.textContent = `Send failed: ${chatViewState.sendError}`;
    composer.append(input, composerActions, sendError);
  } else {
    composer.append(input, composerActions);
  }

  transcriptPane.append(transcriptTitle, transcript, composer);

  const activityPane = document.createElement("aside");
  activityPane.className = "chat-activity-pane";
  const activityTitle = document.createElement("h3");
  activityTitle.textContent = "Live Activity";

  const runID = safeText(chatViewState.currentRunID);
  const status = safeText(chatViewState.currentRunStatus) || "idle";
  const sessionID = safeText(chatViewState.currentSessionID);
  const updatedAt = safeText(chatViewState.currentRunLastUpdatedAt);

  const runMeta = document.createElement("p");
  runMeta.className = "chat-activity-meta";
  runMeta.textContent = `Run: ${runID || "-"} · status ${status}`;

  const sessionMeta = document.createElement("p");
  sessionMeta.className = "chat-activity-meta muted";
  sessionMeta.textContent = `Session: ${sessionID || "(pending)"}${updatedAt ? ` · updated ${formatDateTime(updatedAt)}` : ""}`;

  const latestToolCard = document.createElement("section");
  latestToolCard.className = "chat-activity-card";
  const latestToolTitle = document.createElement("h4");
  latestToolTitle.textContent = "Latest Tool Activity";
  latestToolCard.append(latestToolTitle);

  if (!chatViewState.latestToolActivity) {
    const empty = document.createElement("p");
    empty.className = "muted";
    empty.textContent = "No tool events observed yet.";
    latestToolCard.append(empty);
  } else {
    const event = chatViewState.latestToolActivity;
    const summary = document.createElement("p");
    summary.className = "chat-tool-line";
    summary.textContent = `${event.tool} · ${event.status}${event.ts ? ` · ${formatDateTime(event.ts)}` : ""}`;

    const detail = document.createElement("p");
    detail.className = "muted";
    detail.textContent = compactText(firstNonEmpty(event.summary, event.errorText, event.outputText, event.argsText), 260) || "(no summary)";

    latestToolCard.append(summary, detail);
  }

  const errorCard = document.createElement("section");
  errorCard.className = "chat-activity-card";
  const errorTitle = document.createElement("h4");
  errorTitle.textContent = "Last Error Summary";
  const errorBody = document.createElement("p");
  errorBody.className = chatViewState.lastErrorSummary ? "" : "muted";
  errorBody.textContent = chatViewState.lastErrorSummary || "No recent errors.";
  errorCard.append(errorTitle, errorBody);

  const riskCard = document.createElement("section");
  riskCard.className = `chat-activity-card chat-loop-risk ${chatViewState.loopRisk.level}`;
  const riskTitle = document.createElement("h4");
  riskTitle.textContent = "Loop Risk";

  const riskStatus = document.createElement("p");
  riskStatus.className = "chat-loop-risk-status";
  riskStatus.textContent = `${chatViewState.loopRisk.level.toUpperCase()} · score ${chatViewState.loopRisk.score}`;

  const riskMeta = document.createElement("p");
  riskMeta.className = "muted";
  riskMeta.textContent = `${chatViewState.loopRisk.failureCount}/${chatViewState.loopRisk.windowSize} failures in recent tool window, max repeat ${chatViewState.loopRisk.repeatCount}.`;
  riskCard.append(riskTitle, riskStatus, riskMeta);

  if (chatViewState.loopRisk.reasons.length) {
    const reasonList = document.createElement("ul");
    reasonList.className = "chat-loop-risk-reasons";
    chatViewState.loopRisk.reasons.forEach((reason) => {
      const item = document.createElement("li");
      item.textContent = reason;
      reasonList.append(item);
    });
    riskCard.append(reasonList);
  }

  activityPane.append(activityTitle, runMeta, sessionMeta, latestToolCard, errorCard, riskCard);

  page.append(transcriptPane, activityPane);
  container.append(heading, note, page);

  if (chatViewState.transcriptPinned) {
    transcript.scrollTop = transcript.scrollHeight;
  } else {
    const maxScrollTop = Math.max(0, transcript.scrollHeight - transcript.clientHeight);
    transcript.scrollTop = Math.min(chatViewState.transcriptScrollTop, maxScrollTop);
  }
}

export const chatPage = {
  key: "chat",
  title: "Chat",
  async render({ container, apiClient, store }) {
    chatViewState.container = container;
    chatViewState.apiClient = apiClient;
    chatViewState.store = store;

    renderChatPage();

    if (
      chatViewState.currentRunID &&
      !isTerminalStatus(chatViewState.currentRunStatus) &&
      !chatViewState.polling
    ) {
      startPolling({ runID: chatViewState.currentRunID, sessionID: chatViewState.currentSessionID });
    }
  },
};
