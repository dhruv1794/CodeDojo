const state = {
  session: null,
  activeFile: "",
  highlightedFile: "",
  highlightedLine: 0,
  selectedFile: "",
  selectedStart: 0,
  selectedEnd: 0,
  selectionAnchor: 0,
  selecting: false,
  preflight: null,
  preflightTimer: null,
  selectedMode: "review",
  activePanel: "tests",
  latestTestExit: null,
  latestTestOutput: "",
  latestDiff: "",
  submittedReview: null,
  workflow: {},
  checklist: [],
  timerID: 0,
  busy: "",
};

const $ = (id) => document.getElementById(id);

function cookie(name) {
  const match = document.cookie
    .split(";")
    .map((part) => part.trim())
    .find((part) => part.startsWith(`${name}=`));
  return match ? decodeURIComponent(match.slice(name.length + 1)) : "";
}

async function api(path, options = {}) {
  const res = await fetch(path, {
    headers: { "content-type": "application/json" },
    ...options,
  });
  const data = await res.json();
  if (!res.ok) {
    const err = new Error(data.message || data.error || `request failed: ${res.status}`);
    err.title = data.title || "Request failed";
    err.code = data.code || "";
    err.actions = data.actions || [];
    throw err;
  }
  return data;
}

function output(value) {
  if (!state.session) {
    renderErrorOrText($("preflight"), value);
    return;
  }
  const panel = state.activePanel === "result" ? "tests" : state.activePanel;
  if (value instanceof Error) {
    setPanelError(panel, value);
  } else {
    setPanelOutput(panel, value);
  }
}

function setPanelOutput(panel, text) {
  const target = $(`${panel}-output`);
  if (!target) return;
  target.textContent = text;
  target.classList.toggle("empty-output", !text || text.startsWith("No "));
  target.classList.remove("error-output");
}

function setBusy(name, busy, message = "") {
  if (busy) {
    state.busy = name;
  } else if (state.busy === name) {
    state.busy = "";
  }
  document.body.classList.toggle("is-busy", Boolean(state.busy));
  const disabled = Boolean(state.busy);
  ["start-button", "run-tests", "show-diff", "ask-hint", "submit", "save-file", "refresh-files"].forEach((id) => {
    const el = $(id);
    if (el) {
      el.disabled = disabled || (id === "save-file" && !state.activeFile);
    }
  });
  if (!message) return;
  if (state.session) {
    setPanelOutput(state.activePanel === "result" ? "tests" : state.activePanel, message);
  } else {
    $("preflight").textContent = message;
  }
}

function statusText(ok) {
  return ok ? "Done" : "Needed";
}

function renderErrorOrText(target, value) {
  if (!target) return;
  if (!(value instanceof Error)) {
    target.innerHTML = `<span>${escapeHTML(value)}</span>`;
    target.classList.remove("error-card");
    return;
  }
  target.classList.add("error-card");
  const actions = (value.actions || [])
    .map((action) => `<li>${escapeHTML(action)}</li>`)
    .join("");
  target.innerHTML = `
    <strong>${escapeHTML(value.title || "Request failed")}</strong>
    <span>${escapeHTML(value.message)}</span>
    ${actions ? `<ul>${actions}</ul>` : ""}
  `;
}

function setPanelError(panel, err) {
  const target = $(`${panel}-output`);
  if (!target) return;
  const actions = (err.actions || []).map((action) => `- ${action}`).join("\n");
  target.textContent = `${err.title || "Request failed"}\n${err.message}${actions ? `\n\nTry this instead:\n${actions}` : ""}`;
  target.classList.remove("empty-output");
  target.classList.add("error-output");
}

function appendPanelOutput(panel, text) {
  const target = $(`${panel}-output`);
  if (!target) return;
  const current = target.classList.contains("empty-output") ? "" : target.textContent.trim();
  target.textContent = current ? `${current}\n\n${text}` : text;
  target.classList.remove("empty-output");
}

function setActivePanel(panel) {
  state.activePanel = panel;
  document.querySelectorAll(".tab").forEach((tab) => {
    const active = tab.dataset.panel === panel;
    tab.classList.toggle("active", active);
    tab.setAttribute("aria-selected", active ? "true" : "false");
  });
  document.querySelectorAll(".tab-panel").forEach((section) => {
    section.classList.toggle("hidden", section.id !== `panel-${panel}`);
  });
}

const workflows = {
  newcomer: [
    ["understand", "Understand the task"],
    ["inspect", "Inspect suggested files"],
    ["implement", "Implement the change"],
    ["tests", "Run tests"],
    ["diff", "Review diff"],
    ["submit", "Submit implementation"],
  ],
  reviewer: [
    ["tests", "Run tests"],
    ["inspect", "Inspect suspicious files"],
    ["location", "Select bug location"],
    ["diagnosis", "Write diagnosis"],
    ["submit", "Submit review"],
  ],
};

function nextActionText(key, mode) {
  const actions = {
    understand: "Read the task brief, then inspect the files.",
    inspect: mode === "reviewer" ? "Open a file that looks suspicious." : "Open a file related to the task.",
    implement: "Edit the practice copy, then save your changes.",
    tests: "Run the test suite to get a signal.",
    diff: "Open the diff before submitting.",
    location: "Enter the suspected file and line range.",
    diagnosis: "Write a short diagnosis of the issue.",
    submit: mode === "reviewer" ? "Submit the selected location and diagnosis." : "Submit when the tests and diff look right.",
  };
  return actions[key] || "Keep investigating.";
}

function initializeWorkflow(mode) {
  state.workflow = {};
  state.checklist = workflows[mode] || [];
  if (mode === "newcomer") {
    state.workflow.understand = true;
  }
  renderChecklist();
}

function markStep(key, done = true) {
  if (!state.checklist.length || state.workflow[key] === done) return;
  state.workflow[key] = done;
  renderChecklist();
  if (state.session && state.session.mode === "newcomer") {
    renderLearnSubmitChecklist();
  }
}

function renderChecklist() {
  const list = $("checklist");
  list.textContent = "";
  state.checklist.forEach(([key, label]) => {
    const item = document.createElement("li");
    item.className = state.workflow[key] ? "done" : "";
    item.textContent = label;
    list.appendChild(item);
  });
  updateProgress();
  const next = state.checklist.find(([key]) => !state.workflow[key]);
  const nextAction = $("next-action");
  if (!next) {
    nextAction.textContent = state.checklist.length ? "Session workflow complete." : "Start a session to begin.";
    return;
  }
  nextAction.innerHTML = `<strong>Next</strong><span>${escapeHTML(nextActionText(next[0], state.session && state.session.mode))}</span>`;
}

function updateProgress() {
  const label = $("progress-label");
  if (!label) return;
  const total = state.checklist.length;
  const done = state.checklist.filter(([key]) => state.workflow[key]).length;
  label.textContent = total ? `${done}/${total} steps` : "0/0 steps";
}

function startTimer(startedAt) {
  stopTimer();
  const started = new Date(startedAt).getTime();
  const tick = () => {
    const elapsed = Math.max(0, Math.floor((Date.now() - started) / 1000));
    const minutes = String(Math.floor(elapsed / 60)).padStart(2, "0");
    const seconds = String(elapsed % 60).padStart(2, "0");
    $("timer-label").textContent = `${minutes}:${seconds}`;
  };
  tick();
  state.timerID = window.setInterval(tick, 1000);
}

function stopTimer() {
  if (!state.timerID) return;
  window.clearInterval(state.timerID);
  state.timerID = 0;
}

function commandLabel(command) {
  return command && command.length ? command.join(" ") : "not detected";
}

function escapeHTML(value) {
  return String(value || "")
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#39;");
}

function availabilityText(name, availability) {
  if (!availability) return `${name}: unavailable`;
  if (availability.available) {
    const count = availability.candidate_count ? ` (${availability.candidate_count} candidates)` : "";
    return `${name}: available${count}`;
  }
  return `${name}: ${availability.reason || "unavailable"}`;
}

function availabilityHTML(name, availability) {
  if (!availability) return `<span>${escapeHTML(name)}: unavailable</span>`;
  if (availability.available) {
    return `<span>${escapeHTML(availabilityText(name, availability))}</span>`;
  }
  const actions = (availability.actions || [])
    .map((action) => `<li>${escapeHTML(action)}</li>`)
    .join("");
  return `
    <span>${escapeHTML(name)}: ${escapeHTML(availability.reason || "unavailable")}</span>
    ${actions ? `<ul class="try-actions">${actions}</ul>` : ""}
  `;
}

function supportText(mode, data) {
  const language = data && data.language ? data.language : "detected";
  if (!data) {
    return mode === "review" ? "5-15 minutes · Go repositories" : "15-45 minutes · Commit history";
  }
  if (mode === "review") {
    if (language === "go" && data.review && data.review.available) {
      return "5-15 minutes · Supported for this Go repo";
    }
    return `5-15 minutes · Review support for ${language} is not available yet`;
  }
  if (data.learn && data.learn.available) {
    return `15-45 minutes · Supported for this ${language} repo`;
  }
  return `15-45 minutes · Needs suitable ${language} commits and tests`;
}

function recommendation(data) {
  if (!data) return "review";
  if (data.review && data.review.available) return "review";
  if (data.learn && data.learn.available) return "learn";
  return "";
}

function selectMode(mode) {
  const available = !state.preflight || (state.preflight[mode] && state.preflight[mode].available);
  if (!available) return;
  state.selectedMode = mode;
  $("mode").value = mode;
  document.querySelectorAll(".mode-card").forEach((card) => {
    card.classList.toggle("selected", card.dataset.mode === mode);
  });
}

function clearModeSelection() {
  state.selectedMode = "";
  $("mode").value = "";
  document.querySelectorAll(".mode-card").forEach((card) => {
    card.classList.remove("selected");
  });
}

function updateModeCard(mode, data, recommendedMode) {
  const card = $(`${mode}-card`);
  const status = $(`${mode}-status`);
  const badge = $(`${mode}-badge`);
  const availability = data ? data[mode] : null;
  const isAvailable = !availability || availability.available;
  card.disabled = !isAvailable;
  card.classList.toggle("unavailable", !isAvailable);
  badge.classList.toggle("hidden", mode !== recommendedMode);
  status.textContent = availability ? availabilityText(mode === "learn" ? "Learn" : "Review", availability) : "Check a repository to confirm availability.";
}

function renderPreflight(data) {
  state.preflight = data;
  $("preflight").classList.remove("error-card");
  const recommendedMode = recommendation(data);
  $("learn-language").textContent = supportText("learn", data);
  $("review-language").textContent = supportText("review", data);
  updateModeCard("learn", data, recommendedMode);
  updateModeCard("review", data, recommendedMode);
  if (recommendedMode && (!data[state.selectedMode] || !data[state.selectedMode].available)) {
    selectMode(recommendedMode);
  } else if (!recommendedMode) {
    clearModeSelection();
  } else {
    selectMode(state.selectedMode);
  }
  const recommended = recommendedMode ? (recommendedMode === "review" ? "Review" : "Learn") : "No mode";
  $("preflight").innerHTML = `
    <strong>${escapeHTML(data.repo_name || "Repository")}</strong>
    <span>${escapeHTML(data.language || "unknown")} · tests: ${escapeHTML(commandLabel(data.test_command))} · build: ${escapeHTML(commandLabel(data.build_command))}</span>
    ${availabilityHTML("Learn", data.learn)}
    ${availabilityHTML("Review", data.review)}
    <span>Recommended: ${recommended}</span>
  `;
}

async function preflightRepo() {
  const repo = $("repo").value.trim();
  if (!repo) {
    state.preflight = null;
    $("preflight").textContent = "";
    $("hero-scan-state").textContent = "waiting";
    $("learn-language").textContent = supportText("learn", null);
    $("review-language").textContent = supportText("review", null);
    updateModeCard("learn", null, "review");
    updateModeCard("review", null, "review");
    if (!state.selectedMode) {
      state.selectedMode = "review";
    }
    selectMode(state.selectedMode);
    return;
  }
  $("hero-scan-state").textContent = "scanning";
  setBusy("preflight", true, "checking repository...");
  try {
    renderPreflight(await api("/api/preflight", {
      method: "POST",
      body: JSON.stringify({ repo }),
    }));
    $("hero-scan-state").textContent = "ready";
  } catch (err) {
    state.preflight = null;
    renderErrorOrText($("preflight"), err);
    $("hero-scan-state").textContent = "needs attention";
    $("learn-language").textContent = supportText("learn", null);
    $("review-language").textContent = supportText("review", null);
    updateModeCard("learn", { learn: { available: false, reason: err.title || "repository check failed" } }, "");
    updateModeCard("review", { review: { available: false, reason: err.title || "repository check failed" } }, "");
    clearModeSelection();
  } finally {
    setBusy("preflight", false);
  }
}

function schedulePreflight() {
  clearTimeout(state.preflightTimer);
  state.preflightTimer = setTimeout(preflightRepo, 350);
}

function updateLineNumbers() {
  const textarea = $("file-content");
  const gutter = $("line-numbers");
  const count = Math.max(1, textarea.value.split("\n").length);
  const lines = [];
  for (let i = 1; i <= count; i += 1) {
    const highlighted = state.activeFile === state.highlightedFile && i === state.highlightedLine;
    const selected = state.activeFile === state.selectedFile && lineInSelectedRange(i);
    const classes = ["line-number"];
    if (highlighted) classes.push("highlight");
    if (selected) classes.push("selected");
    lines.push(`<button class="${classes.join(" ")}" type="button" data-line="${i}" aria-label="Select line ${i}">${i}</button>`);
  }
  gutter.innerHTML = lines.join("");
  gutter.scrollTop = textarea.scrollTop;
}

function lineInSelectedRange(line) {
  if (!state.selectedStart || !state.selectedEnd) return false;
  const start = Math.min(state.selectedStart, state.selectedEnd);
  const end = Math.max(state.selectedStart, state.selectedEnd);
  return line >= start && line <= end;
}

function highlightLine(path, line) {
  state.highlightedFile = path || "";
  state.highlightedLine = Number(line) || 0;
  updateLineNumbers();
}

function setReviewSelection(path, start, end = start) {
  if (!state.session || state.session.mode !== "reviewer" || !path) return;
  const first = Number(start) || 0;
  const last = Number(end) || first;
  if (first <= 0) return;
  state.selectedFile = path;
  state.selectedStart = Math.min(first, last);
  state.selectedEnd = Math.max(first, last);
  $("review-file").value = path;
  $("review-start").value = state.selectedStart;
  $("review-end").value = state.selectedEnd;
  updateReviewSubmissionProgress();
  updateLineNumbers();
}

function clearReviewSelection() {
  state.selectedFile = "";
  state.selectedStart = 0;
  state.selectedEnd = 0;
  state.selectionAnchor = 0;
  $("review-file").value = "";
  $("review-start").value = "";
  $("review-end").value = "";
  updateReviewSubmissionProgress();
  updateLineNumbers();
}

function useCurrentFileForReview() {
  if (!state.activeFile) return;
  const line = state.selectedStart || 1;
  setReviewSelection(state.activeFile, line, line);
  state.selectionAnchor = line;
}

function renderSession(session) {
  state.session = session;
  state.activeFile = "";
  state.highlightedFile = "";
  state.highlightedLine = 0;
  state.selectedFile = "";
  state.selectedStart = 0;
  state.selectedEnd = 0;
  state.selectionAnchor = 0;
  state.selecting = false;
  state.activePanel = "tests";
  state.latestTestExit = null;
  state.latestTestOutput = "";
  state.latestDiff = "";
  state.submittedReview = null;
  $("setup").classList.add("hidden");
  $("workspace").classList.remove("hidden");
  $("mode-label").textContent = session.mode === "reviewer" ? "Review practice" : "Learn practice";
  $("task-title").textContent = session.task;
  $("difficulty-label").textContent = `D${session.difficulty}`;
  $("streak-label").textContent = `Streak ${session.streak}`;
  $("hint-label").textContent = `Hint budget ${session.hints_used}/${session.hint_budget}`;
  startTimer(session.started_at);
  $("review-submit").classList.toggle("hidden", session.mode !== "reviewer");
  $("learn-submit").classList.toggle("hidden", session.mode !== "newcomer");
  $("file-content").placeholder = session.mode === "reviewer" ? "Open a file to inspect the practice copy." : "Open a file to make changes in the practice copy.";
  $("suggested-title").textContent = session.mode === "reviewer" ? "Suspicious files" : "Suggested files";
  $("diff-title").textContent = session.mode === "reviewer" ? "Practice copy diff" : "Practice copy diff";
  $("diff-help").textContent = session.mode === "reviewer" ? "Inspect changes in the practice copy without revealing the hidden bug." : "Compare your implementation before submitting for the reference solution.";
  $("hint-help").textContent = `Spend from the hint budget when you want a nudge. ${session.hint_budget - session.hints_used} remaining.`;
  setPanelOutput("tests", "No test run yet.");
  setPanelOutput("diff", session.mode === "reviewer" ? "Open this panel when you want to inspect the practice copy." : "Open this panel after editing to review your implementation.");
  setPanelOutput("hints", "No hints used yet.");
  renderEmptyResult();
  renderLearnSubmitChecklist();
  updateReviewSubmissionProgress();
  setActivePanel("tests");
  renderTaskFiles(session.task_files || []);
  initializeWorkflow(session.mode);
}

function renderEmptyResult() {
  $("result-summary").textContent = "Submit a session to see your score, feedback, and reveal.";
  $("result-summary").className = "result-summary empty-state";
  $("result-details").textContent = "";
  setPanelOutput("result", "No result yet.");
}

function renderLearnSubmitChecklist() {
  const list = $("learn-presubmit");
  if (!list) return;
  const checks = [
    ["Tests run", Boolean(state.workflow.tests), state.latestTestExit === null ? "No test result yet" : `Latest exit code ${state.latestTestExit}`],
    ["Diff reviewed", Boolean(state.workflow.diff), state.latestDiff ? "Practice copy diff opened" : "Open Diff before submitting"],
    ["Hints used", true, `${state.session ? state.session.hints_used : 0} used`],
  ];
  list.textContent = "";
  checks.forEach(([label, ok, detail]) => {
    const item = document.createElement("li");
    item.className = ok ? "done" : "";
    item.innerHTML = `<strong>${escapeHTML(label)}</strong><span>${escapeHTML(statusText(ok))} · ${escapeHTML(detail)}</span>`;
    list.appendChild(item);
  });
}

function renderTaskFiles(files) {
  const list = $("suggested-files");
  list.textContent = "";
  list.classList.toggle("empty-state", files.length === 0);
  if (files.length === 0) {
    list.textContent = state.session && state.session.mode === "reviewer"
      ? "Run tests and inspect the file tree for suspicious source files."
      : "Use the file tree and changed tests to orient yourself.";
    return;
  }
  files.forEach((file) => {
    const button = document.createElement("button");
    button.type = "button";
    button.className = "suggested-file";
    button.innerHTML = `<strong>${escapeHTML(file.path)}</strong><span>${escapeHTML(file.reason || "")}</span>`;
    button.addEventListener("click", () => openFile(file.path));
    list.appendChild(button);
  });
}

async function refreshFiles() {
  setBusy("files", true);
  try {
    const data = await api(`/api/sessions/${state.session.id}/files`);
    const list = $("file-list");
    list.textContent = "";
    list.classList.toggle("empty-state", data.files.length === 0);
    if (data.files.length === 0) {
      list.textContent = "No files were found in this practice copy.";
      return;
    }
    data.files.forEach((entry) => {
      const button = document.createElement("button");
      button.className = entry.dir ? "file dir" : "file";
      button.type = "button";
      button.textContent = entry.dir ? `${entry.path}/` : entry.path;
      if (!entry.dir) {
        button.addEventListener("click", () => openFile(entry.path));
      }
      list.appendChild(button);
    });
  } catch (err) {
    output(err);
  } finally {
    setBusy("files", false);
  }
}

async function openFile(path) {
  setBusy("file", true);
  try {
    const data = await api(`/api/sessions/${state.session.id}/files/${encodeURI(path)}`);
    state.activeFile = path;
    $("active-file").textContent = path;
    $("file-content").disabled = false;
    $("save-file").disabled = false;
    $("file-content").value = data.content;
    updateLineNumbers();
    markStep("inspect");
    if (state.session.mode === "reviewer") {
      if (!state.selectedFile) {
        $("review-file").value = path;
      }
      updateReviewSubmissionProgress();
    }
  } catch (err) {
    output(err);
  } finally {
    setBusy("file", false);
  }
}

async function saveFile() {
  if (!state.activeFile) return;
  setBusy("save", true);
  try {
    await api(`/api/sessions/${state.session.id}/files/${encodeURI(state.activeFile)}`, {
      method: "PUT",
      body: JSON.stringify({ content: $("file-content").value }),
    });
    markStep("implement");
    setPanelOutput("diff", `Saved ${state.activeFile}. Open the diff when you are ready to review the practice copy.`);
  } catch (err) {
    output(err);
  } finally {
    setBusy("save", false);
  }
}

async function startSession(event) {
  event.preventDefault();
  const mode = state.selectedMode;
  if (!mode) {
    output("No mode is available for this repository.");
    return;
  }
  if (state.preflight && state.preflight[mode] && !state.preflight[mode].available) {
    output(state.preflight[mode].reason || `${mode} mode is not available for this repository`);
    return;
  }
  const payload = {
    repo: $("repo").value,
    difficulty: Number($("difficulty").value),
    hint_budget: Number($("hint-budget").value),
  };
  setBusy("start", true, "starting session...");
  try {
    const session = await api(`/api/sessions/${mode}`, {
      method: "POST",
      body: JSON.stringify(payload),
    });
    renderSession(session);
    await refreshFiles();
    output(state.session.mode === "reviewer" ? "Session ready. Run tests first, then inspect suspicious files." : "Session ready. Read the task, inspect files, and make your implementation.");
  } catch (err) {
    output(err);
  } finally {
    setBusy("start", false);
  }
}

async function runTests() {
  setActivePanel("tests");
  setBusy("tests", true, "Running tests...");
  try {
    const result = await api(`/api/sessions/${state.session.id}/tests`, { method: "POST", body: "{}" });
    state.latestTestExit = result.exit_code;
    state.latestTestOutput = `${result.stdout || ""}${result.stderr || ""}`.trim();
    markStep("tests");
    setPanelOutput("tests", `${result.stdout || ""}${result.stderr || ""}\nexit code: ${result.exit_code}`);
  } catch (err) {
    state.latestTestExit = null;
    state.latestTestOutput = err.message;
    setPanelError("tests", err);
    renderLearnSubmitChecklist();
  } finally {
    setBusy("tests", false);
  }
}

async function showDiff() {
  setActivePanel("diff");
  setBusy("diff", true, "Loading diff...");
  try {
    const data = await api(`/api/sessions/${state.session.id}/diff`);
    state.latestDiff = data.diff || "";
    markStep("diff");
    setPanelOutput("diff", data.diff || "(no practice copy edits)");
  } catch (err) {
    setPanelError("diff", err);
  } finally {
    setBusy("diff", false);
  }
}

async function askHint() {
  setActivePanel("hints");
  setBusy("hint", true);
  try {
    const data = await api(`/api/sessions/${state.session.id}/hints`, {
      method: "POST",
      body: JSON.stringify({ level: $("hint-level").value }),
    });
    state.session.hints_used = data.hints_used;
    $("hint-label").textContent = `Hint budget ${data.hints_used}/${state.session.hint_budget}`;
    $("hint-help").textContent = `Spend from the hint budget when you want a nudge. ${state.session.hint_budget - data.hints_used} remaining.`;
    if (state.session.mode === "newcomer") {
      markStep("understand");
      renderLearnSubmitChecklist();
    } else if (state.session.mode === "reviewer") {
      markStep("inspect");
    }
    appendPanelOutput("hints", `-${data.cost}: ${data.hint}`);
  } catch (err) {
    appendPanelOutput("hints", `${err.title || "Request failed"}\n${err.message}${err.actions && err.actions.length ? `\n\nTry this instead:\n- ${err.actions.join("\n- ")}` : ""}`);
  } finally {
    setBusy("hint", false);
  }
}

async function submit() {
  setActivePanel("submit");
  setBusy("submit", true, "Grading submission...");
  try {
    let body = "{}";
    if (state.session.mode === "reviewer") {
      updateReviewSubmissionProgress();
      state.submittedReview = {
        file: $("review-file").value,
        start: Number($("review-start").value),
        end: Number($("review-end").value) || Number($("review-start").value),
        issue: $("review-operator").value,
        diagnosis: $("review-diagnosis").value,
      };
      body = JSON.stringify({
        file_path: $("review-file").value,
        start_line: Number($("review-start").value),
        end_line: Number($("review-end").value),
        operator_class: $("review-operator").value,
        diagnosis: $("review-diagnosis").value,
      });
    }
    const result = await api(`/api/sessions/${state.session.id}/submit`, {
      method: "POST",
      body,
    });
    markStep("submit");
    if (result.reveal && result.reveal.file && result.reveal.line) {
      await openFile(result.reveal.file);
      highlightLine(result.reveal.file, Number(result.reveal.line));
    }
    renderResult(result);
    setActivePanel("result");
  } catch (err) {
    setPanelError("result", err);
    setActivePanel("result");
  } finally {
    setBusy("submit", false);
  }
}

function renderResult(result) {
  const title = state.session.mode === "reviewer" ? "Hidden bug reveal" : "Reference solution reveal";
  $("result-summary").className = "result-summary";
  $("result-summary").innerHTML = `
    <strong>Score ${escapeHTML(result.score)}</strong>
    <span>${escapeHTML(title)}</span>
    <span>${escapeHTML(result.feedback || "No coach feedback returned.")}</span>
  `;
  renderResultDetails(result);
  const reveal = renderReveal(result.reveal);
  setPanelOutput("result", reveal || "No reveal details returned.");
}

function renderResultDetails(result) {
  const details = $("result-details");
  const breakdown = Object.entries(result.breakdown || {})
    .map(([key, value]) => `<li><span>${escapeHTML(humanizeResultKey(key))}</span><strong>${escapeHTML(value)}</strong></li>`)
    .join("");
  const testExit = result.test_exit_code !== undefined && result.test_exit_code !== 0 ? result.test_exit_code : state.latestTestExit;
  const submitted = state.session.mode === "reviewer" ? reviewSubmissionHTML() : learnSubmissionHTML(result);
  details.innerHTML = `
    <section class="result-card">
      <h4>Score breakdown</h4>
      <ul class="score-list">${breakdown || "<li>No score breakdown returned.</li>"}</ul>
    </section>
    <section class="result-card">
      <h4>Submitted work</h4>
      ${submitted}
    </section>
    <section class="result-card">
      <h4>Test result</h4>
      <p>${testExit === null || testExit === undefined ? "No test result captured before submission." : `Latest exit code ${escapeHTML(testExit)}`}</p>
    </section>
  `;
}

function reviewSubmissionHTML() {
  const submitted = state.submittedReview || {};
  const end = submitted.end || submitted.start || "?";
  return `
    <p>${escapeHTML(submitted.file || "(no file selected)")}:${escapeHTML(submitted.start || "?")}-${escapeHTML(end)}</p>
    <p>${escapeHTML(submitted.issue || "No issue type entered.")}</p>
    <p>${escapeHTML(submitted.diagnosis || "No diagnosis entered.")}</p>
  `;
}

function learnSubmissionHTML(result) {
  const exitCode = result.test_exit_code !== undefined ? result.test_exit_code : state.latestTestExit;
  return `
    <p>Tests ${state.workflow.tests ? "run" : "not run"}${exitCode === null || exitCode === undefined ? "" : `, latest exit code ${escapeHTML(exitCode)}`}.</p>
    <p>Diff ${state.workflow.diff ? "reviewed" : "not reviewed"}.</p>
    <p>Hints used: ${escapeHTML(state.session.hints_used)}.</p>
  `;
}

function renderReveal(reveal) {
  if (!reveal) return "";
  if (state.session.mode === "reviewer") {
    const parts = [];
    const submitted = state.submittedReview || {};
    if (submitted.file || submitted.start) {
      parts.push(`Your selection: ${submitted.file || "(none)"}:${submitted.start || "?"}-${submitted.end || submitted.start || "?"}`);
    }
    if (reveal.file || reveal.line) {
      parts.push(`Actual hidden bug: ${reveal.file || "(unknown)"}:${reveal.line || "?"}`);
    }
    if (reveal.operator) {
      parts.push(`Issue type: ${reveal.operator}`);
    }
    return parts.join("\n");
  }
  if (reveal.reference_diff) {
    return `Your practice copy diff:\n${state.latestDiff || "(diff was not reviewed before submission)"}\n\nReference solution diff:\n${reveal.reference_diff}`;
  }
  return `Reveal:\n${JSON.stringify(reveal, null, 2)}`;
}

function humanizeResultKey(key) {
  const labels = {
    correctness: "Correctness",
    approach: "Approach",
    tests: "Test quality",
    hints: "Hint cost",
    file: "File",
    line: "Line",
    operator: "Issue type",
    diagnosis: "Diagnosis",
  };
  return labels[key] || key;
}

function startAnotherSession() {
  state.session = null;
  state.activeFile = "";
  state.highlightedFile = "";
  state.highlightedLine = 0;
  state.selectedFile = "";
  state.selectedStart = 0;
  state.selectedEnd = 0;
  state.selectionAnchor = 0;
  state.selecting = false;
  state.activePanel = "tests";
  state.latestTestExit = null;
  state.latestTestOutput = "";
  state.latestDiff = "";
  state.submittedReview = null;
  stopTimer();
  setBusy(state.busy, false);
  $("workspace").classList.add("hidden");
  $("setup").classList.remove("hidden");
  $("active-file").textContent = "No file selected";
  $("file-content").value = "";
  $("file-content").disabled = true;
  $("save-file").disabled = true;
  $("review-file").value = "";
  $("review-start").value = "";
  $("review-end").value = "";
  $("review-operator").value = "";
  $("review-diagnosis").value = "";
  $("file-list").textContent = "Start a session to load files.";
  $("file-list").classList.add("empty-state");
  updateLineNumbers();
  preflightRepo();
}

$("start-form").addEventListener("submit", startSession);
$("refresh-files").addEventListener("click", refreshFiles);
$("save-file").addEventListener("click", saveFile);
$("run-tests").addEventListener("click", runTests);
$("show-diff").addEventListener("click", showDiff);
$("ask-hint").addEventListener("click", askHint);
$("submit").addEventListener("click", submit);
$("start-another").addEventListener("click", startAnotherSession);
$("clear-selection").addEventListener("click", clearReviewSelection);
$("use-current-file").addEventListener("click", useCurrentFileForReview);
$("file-content").addEventListener("input", updateLineNumbers);
$("file-content").addEventListener("input", () => markStep("implement"));
$("file-content").addEventListener("scroll", () => {
  $("line-numbers").scrollTop = $("file-content").scrollTop;
});
$("line-numbers").addEventListener("mousedown", (event) => {
  const target = event.target.closest(".line-number");
  if (!target || !state.activeFile || !state.session || state.session.mode !== "reviewer") return;
  event.preventDefault();
  const line = Number(target.dataset.line);
  state.selecting = true;
  state.selectionAnchor = event.shiftKey && state.selectionAnchor ? state.selectionAnchor : line;
  setReviewSelection(state.activeFile, state.selectionAnchor, line);
});
$("line-numbers").addEventListener("mouseover", (event) => {
  if (!state.selecting || !state.activeFile) return;
  const target = event.target.closest(".line-number");
  if (!target) return;
  setReviewSelection(state.activeFile, state.selectionAnchor, Number(target.dataset.line));
});
document.addEventListener("mouseup", () => {
  state.selecting = false;
});
document.addEventListener("keydown", (event) => {
  if (event.defaultPrevented) return;
  const target = event.target;
  const isTyping = target && ["INPUT", "TEXTAREA", "SELECT"].includes(target.tagName);
  if (event.altKey && ["1", "2", "3", "4", "5"].includes(event.key)) {
    event.preventDefault();
    const panels = ["tests", "diff", "hints", "submit", "result"];
    setActivePanel(panels[Number(event.key) - 1]);
    return;
  }
  if ((event.metaKey || event.ctrlKey) && event.key === "Enter") {
    event.preventDefault();
    if (state.session) {
      submit();
    } else {
      $("start-form").requestSubmit();
    }
    return;
  }
  if (isTyping || !state.session || state.busy) return;
  switch (event.key.toLowerCase()) {
    case "t":
      event.preventDefault();
      runTests();
      break;
    case "d":
      event.preventDefault();
      showDiff();
      break;
    case "h":
      event.preventDefault();
      askHint();
      break;
    case "s":
      if (state.activeFile) {
        event.preventDefault();
        saveFile();
      }
      break;
  }
});
$("repo").addEventListener("input", schedulePreflight);
$("repo").addEventListener("blur", preflightRepo);
document.querySelectorAll(".mode-card").forEach((card) => {
  card.addEventListener("click", () => selectMode(card.dataset.mode));
});
document.querySelectorAll(".tab").forEach((tab) => {
  tab.addEventListener("click", () => setActivePanel(tab.dataset.panel));
});

function updateReviewSubmissionProgress() {
  if (!state.session || state.session.mode !== "reviewer") return;
  const hasLocation = $("review-file").value.trim() && Number($("review-start").value) > 0;
  const hasDiagnosis = $("review-diagnosis").value.trim().length > 0;
  state.selectedFile = $("review-file").value.trim();
  state.selectedStart = Number($("review-start").value) || 0;
  state.selectedEnd = Number($("review-end").value) || state.selectedStart;
  renderReviewSelectionSummary(hasLocation, hasDiagnosis);
  markStep("location", Boolean(hasLocation));
  markStep("diagnosis", Boolean(hasDiagnosis));
  updateLineNumbers();
}

function renderReviewSelectionSummary(hasLocation, hasDiagnosis) {
  const summary = $("review-selection-summary");
  if (!summary) return;
  summary.classList.toggle("empty-state", !hasLocation);
  if (!hasLocation) {
    summary.textContent = "No hidden bug location selected yet.";
    return;
  }
  const end = state.selectedEnd || state.selectedStart;
  summary.innerHTML = `
    <strong>${escapeHTML(state.selectedFile)}</strong>
    <span>Selected lines ${escapeHTML(state.selectedStart)}-${escapeHTML(end)}</span>
    <span>${hasDiagnosis ? "Diagnosis ready" : "Diagnosis needed"}</span>
  `;
}

["review-file", "review-start", "review-end", "review-diagnosis"].forEach((id) => {
  $(id).addEventListener("input", updateReviewSubmissionProgress);
});

const defaultRepo = cookie("codedojo_repo");
if (defaultRepo) {
  $("repo").value = defaultRepo;
  preflightRepo();
}

updateLineNumbers();
selectMode(state.selectedMode);
