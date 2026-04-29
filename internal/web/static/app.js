const state = {
  session: null,
  activeFile: "",
  highlightedFile: "",
  highlightedLine: 0,
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
    throw new Error(data.error || `request failed: ${res.status}`);
  }
  return data;
}

function output(text) {
  $("output").textContent = text;
}

function updateLineNumbers() {
  const textarea = $("file-content");
  const gutter = $("line-numbers");
  const count = Math.max(1, textarea.value.split("\n").length);
  const lines = [];
  for (let i = 1; i <= count; i += 1) {
    const highlighted = state.activeFile === state.highlightedFile && i === state.highlightedLine;
    lines.push(`<div class="line-number${highlighted ? " highlight" : ""}">${i}</div>`);
  }
  gutter.innerHTML = lines.join("");
  gutter.scrollTop = textarea.scrollTop;
}

function highlightLine(path, line) {
  state.highlightedFile = path || "";
  state.highlightedLine = Number(line) || 0;
  updateLineNumbers();
}

function renderSession(session) {
  state.session = session;
  $("setup").classList.add("hidden");
  $("workspace").classList.remove("hidden");
  $("mode-label").textContent = `${session.mode} mode`;
  $("task-title").textContent = session.task;
  $("difficulty-label").textContent = `D${session.difficulty}`;
  $("streak-label").textContent = `Streak ${session.streak}`;
  $("hint-label").textContent = `Hints ${session.hints_used}/${session.hint_budget}`;
  $("review-submit").classList.toggle("hidden", session.mode !== "reviewer");
}

async function refreshFiles() {
  const data = await api(`/api/sessions/${state.session.id}/files`);
  const list = $("file-list");
  list.textContent = "";
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
}

async function openFile(path) {
  const data = await api(`/api/sessions/${state.session.id}/files/${encodeURI(path)}`);
  state.activeFile = path;
  $("active-file").textContent = path;
  $("file-content").disabled = false;
  $("save-file").disabled = false;
  $("file-content").value = data.content;
  updateLineNumbers();
  if (state.session.mode === "reviewer") {
    $("review-file").value = path;
  }
}

async function saveFile() {
  if (!state.activeFile) return;
  await api(`/api/sessions/${state.session.id}/files/${encodeURI(state.activeFile)}`, {
    method: "PUT",
    body: JSON.stringify({ content: $("file-content").value }),
  });
  output(`saved ${state.activeFile}`);
}

async function startSession(event) {
  event.preventDefault();
  const mode = $("mode").value;
  const payload = {
    repo: $("repo").value,
    difficulty: Number($("difficulty").value),
    hint_budget: Number($("hint-budget").value),
  };
  output("starting session...");
  try {
    const session = await api(`/api/sessions/${mode}`, {
      method: "POST",
      body: JSON.stringify(payload),
    });
    renderSession(session);
    await refreshFiles();
    output("session ready");
  } catch (err) {
    output(err.message);
  }
}

async function runTests() {
  output("running tests...");
  try {
    const result = await api(`/api/sessions/${state.session.id}/tests`, { method: "POST", body: "{}" });
    output(`${result.stdout || ""}${result.stderr || ""}\nexit code: ${result.exit_code}`);
  } catch (err) {
    output(err.message);
  }
}

async function showDiff() {
  try {
    const data = await api(`/api/sessions/${state.session.id}/diff`);
    output(data.diff);
  } catch (err) {
    output(err.message);
  }
}

async function askHint() {
  try {
    const data = await api(`/api/sessions/${state.session.id}/hints`, {
      method: "POST",
      body: JSON.stringify({ level: $("hint-level").value }),
    });
    state.session.hints_used = data.hints_used;
    $("hint-label").textContent = `Hints ${data.hints_used}/${state.session.hint_budget}`;
    output(`hint (-${data.cost}): ${data.hint}`);
  } catch (err) {
    output(err.message);
  }
}

async function submit() {
  try {
    let body = "{}";
    if (state.session.mode === "reviewer") {
      highlightLine($("review-file").value, Number($("review-start").value));
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
    if (result.reveal && result.reveal.file && result.reveal.line) {
      await openFile(result.reveal.file);
      highlightLine(result.reveal.file, Number(result.reveal.line));
    }
    const breakdown = Object.entries(result.breakdown)
      .map(([key, value]) => `${key}: ${value}`)
      .join("\n");
    const reveal = result.reveal ? `\n\nReveal:\n${JSON.stringify(result.reveal, null, 2)}` : "";
    output(`score: ${result.score}\n${breakdown}\n\n${result.feedback || ""}${reveal}`);
  } catch (err) {
    output(err.message);
  }
}

$("start-form").addEventListener("submit", startSession);
$("refresh-files").addEventListener("click", refreshFiles);
$("save-file").addEventListener("click", saveFile);
$("run-tests").addEventListener("click", runTests);
$("show-diff").addEventListener("click", showDiff);
$("ask-hint").addEventListener("click", askHint);
$("submit").addEventListener("click", submit);
$("file-content").addEventListener("input", updateLineNumbers);
$("file-content").addEventListener("scroll", () => {
  $("line-numbers").scrollTop = $("file-content").scrollTop;
});

const defaultRepo = cookie("codedojo_repo");
if (defaultRepo) {
  $("repo").value = defaultRepo;
}

updateLineNumbers();
