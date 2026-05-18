import React, { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { createRoot } from "react-dom/client";
import Editor from "@monaco-editor/react";
import "./styles.css";

const BELTS = [
  { name: "white", label: "White", css: "var(--belt-white)" },
  { name: "yellow", label: "Yellow", css: "var(--belt-yellow)" },
  { name: "green", label: "Green", css: "var(--belt-green)" },
  { name: "brown", label: "Brown", css: "var(--belt-brown)" },
  { name: "black", label: "Black", css: "var(--belt-black)" },
];

const RECENT_REPOS_KEY = "codedojo_recent_repos";
const ACTIVE_SESSION_KEY = "codedojo_active_session";
const MAX_RECENT_REPOS = 6;

function beltForDifficulty(d) {
  return BELTS[Math.max(0, Math.min(d || 1, 5) - 1)];
}

const initialWorkflow = {
  newcomer: [
    ["understand", "Understand the task"],
    ["inspect", "Inspect suggested files"],
    ["implement", "Implement the change"],
    ["tests", "Run tests"],
    ["diff", "Review diff"],
    ["submit", "Submit implementation"],
  ],
  reviewer: [
    ["forecast", "Predict bug location"],
    ["tests", "Run tests"],
    ["inspect", "Inspect suspicious files"],
    ["location", "Select bug location"],
    ["diagnosis", "Write diagnosis"],
    ["submit", "Submit review"],
  ],
};

const nextActionText = {
  forecast: "Before running tests, predict which file the bug is in.",
  understand: "Read the kata brief, then inspect the files.",
  inspect: "Open a file that looks relevant.",
  implement: "Edit the practice copy, then save your changes.",
  tests: "Run the test suite to get a signal.",
  diff: "Open the diff before submitting.",
  location: "Select the suspected line or range.",
  diagnosis: "Write a short diagnosis of the issue.",
  submit: "Submit when the evidence looks ready.",
};

function cookie(name) {
  const match = document.cookie
    .split(";")
    .map((part) => part.trim())
    .find((part) => part.startsWith(`${name}=`));
  return match ? decodeURIComponent(match.slice(name.length + 1)) : "";
}

function loadRecentRepos() {
  try {
    const parsed = JSON.parse(window.localStorage.getItem(RECENT_REPOS_KEY) || "[]");
    return Array.isArray(parsed) ? parsed.filter((value) => typeof value === "string" && value.trim()).slice(0, MAX_RECENT_REPOS) : [];
  } catch {
    return [];
  }
}

function saveRecentRepos(repos) {
  try {
    window.localStorage.setItem(RECENT_REPOS_KEY, JSON.stringify(repos.slice(0, MAX_RECENT_REPOS)));
  } catch {
    // Recent repositories are only a convenience; ignore private browsing/storage errors.
  }
}

function rememberRecentRepo(value, current = loadRecentRepos()) {
  const cleaned = value.trim();
  if (!cleaned) return current;
  const next = [cleaned, ...current.filter((item) => item !== cleaned)].slice(0, MAX_RECENT_REPOS);
  saveRecentRepos(next);
  return next;
}

function loadActiveSessionID() {
  try {
    return window.localStorage.getItem(ACTIVE_SESSION_KEY) || "";
  } catch {
    return "";
  }
}

function saveActiveSessionID(id) {
  try {
    if (id) window.localStorage.setItem(ACTIVE_SESSION_KEY, id);
    else window.localStorage.removeItem(ACTIVE_SESSION_KEY);
  } catch {
    // Active-session recovery is a convenience; ignore private browsing/storage errors.
  }
}

function initialRepoFromURL() {
  const params = new URLSearchParams(window.location.search);
  return params.get("repo") || "";
}

function initialSenseiPackFromURL() {
  const params = new URLSearchParams(window.location.search);
  return params.get("kata") || params.get("pack") || "";
}

function isRemoteRepo(value) {
  const trimmed = value.trim();
  return /^https?:\/\/.+/i.test(trimmed) || /^ssh:\/\/.+/i.test(trimmed) || /^git@[^:]+:.+/i.test(trimmed);
}

function setupModeForSession(mode) {
  return mode === "newcomer" ? "learn" : "review";
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

function commandLabel(command) {
  return command && command.length ? command.join(" ") : "not detected";
}

function availabilityText(name, availability) {
  if (!availability) return `${name}: unavailable`;
  if (availability.available) {
    const count = availability.candidate_count ? ` (${availability.candidate_count} candidates)` : "";
    return `${name}: available${count}`;
  }
  return `${name}: ${availability.reason || "unavailable"}`;
}

function formatElapsed(startedAt, now) {
  if (!startedAt) return "00:00";
  const started = new Date(startedAt).getTime();
  const elapsed = Math.max(0, Math.floor((now - started) / 1000));
  const minutes = String(Math.floor(elapsed / 60)).padStart(2, "0");
  const seconds = String(elapsed % 60).padStart(2, "0");
  return `${minutes}:${seconds}`;
}

function languageForPath(path) {
  if (path.endsWith(".go")) return "go";
  if (path.endsWith(".py")) return "python";
  if (path.endsWith(".ts") || path.endsWith(".tsx")) return "typescript";
  if (path.endsWith(".js") || path.endsWith(".jsx")) return "javascript";
  if (path.endsWith(".rs")) return "rust";
  if (path.endsWith(".json")) return "json";
  if (path.endsWith(".md")) return "markdown";
  return "plaintext";
}

function App() {
  const [repo, setRepo] = useState("");
  const [mode, setMode] = useState("review");
  const [difficulty, setDifficulty] = useState(3);
  const [hintBudget, setHintBudget] = useState(3);
  const [preflight, setPreflight] = useState(null);
  const [preflightError, setPreflightError] = useState(null);
  const [session, setSession] = useState(null);
  const [files, setFiles] = useState([]);
  const [activeFile, setActiveFile] = useState("");
  const [content, setContent] = useState("");
  const [activePanel, setActivePanel] = useState("tests");
  const [outputs, setOutputs] = useState({
    tests: "No test run yet.",
    diff: "Open this panel after making edits or when you want to inspect the practice copy.",
    hints: "No hints used yet.",
    result: "No result yet.",
  });
  const [errors, setErrors] = useState({});
  const [hintLevel, setHintLevel] = useState("nudge");
  const [busy, setBusy] = useState("");
  const [workflow, setWorkflow] = useState({});
  const [latestTestExit, setLatestTestExit] = useState(null);
  const [latestDiff, setLatestDiff] = useState("");
  const [selectedFile, setSelectedFile] = useState("");
  const [selectedStart, setSelectedStart] = useState(0);
  const [selectedEnd, setSelectedEnd] = useState(0);
  const [reviewOperator, setReviewOperator] = useState("");
  const [reviewDiagnosis, setReviewDiagnosis] = useState("");
  const [result, setResult] = useState(null);
  const [now, setNow] = useState(Date.now());
  const [forecastFile, setForecastFile] = useState("");
  const [wrongFirstMode, setWrongFirstMode] = useState(false);
  const [hintWallet, setHintWallet] = useState(0);
  const [coachMessage, setCoachMessage] = useState("");
  const editorRef = useRef(null);
  const [beltPromotion, setBeltPromotion] = useState(null);
  const [mistakeIndex, setMistakeIndex] = useState(null);
  const [kataHistory, setKataHistory] = useState(null);
  const [repoBrowserOpen, setRepoBrowserOpen] = useState(false);
  const [repoBrowser, setRepoBrowser] = useState(null);
  const [repoBrowserError, setRepoBrowserError] = useState(null);
  const [repoBrowserShowHidden, setRepoBrowserShowHidden] = useState(false);
  const [recentRepos, setRecentRepos] = useState([]);
  const [openedRepo, setOpenedRepo] = useState("");
  const [senseiPack, setSenseiPack] = useState("");

  const sessionMode = session?.mode || "";
  const checklist = initialWorkflow[sessionMode] || [];
  const doneSteps = checklist.filter(([key]) => workflow[key]).length;
  const selectedAvailability = mode === "review" ? preflight?.review : preflight?.learn;
  const remoteRepo = isRemoteRepo(repo);
  const remoteNeedsOpen = remoteRepo && openedRepo !== repo.trim();
  const senseiMode = Boolean(senseiPack.trim());
  const canStart = senseiMode
    ? !busy && senseiPack.trim()
    : repo.trim() && !busy && !remoteNeedsOpen && (!preflight || selectedAvailability?.available);

  useEffect(() => {
    const storedRepos = loadRecentRepos();
    const fromURL = initialRepoFromURL();
    const packFromURL = initialSenseiPackFromURL();
    const fromCookie = fromURL || cookie("codedojo_repo");
    if (packFromURL) setSenseiPack(packFromURL);
    if (fromCookie) {
      setRepo(fromCookie);
      if (fromURL) setOpenedRepo(fromURL.trim());
      setRecentRepos(rememberRecentRepo(fromCookie, storedRepos));
    } else {
      setRecentRepos(storedRepos);
    }
  }, []);

  useEffect(() => {
    const savedID = loadActiveSessionID();
    if (!savedID) return undefined;
    let cancelled = false;
    (async () => {
      setBusy("resume");
      try {
        const data = await api(`/api/sessions/${savedID}`);
        if (cancelled) return;
        if (data.done) {
          saveActiveSessionID("");
          return;
        }
        setSession(data);
        setRepo(data.repo || "");
        setMode(setupModeForSession(data.mode));
        setWorkflow(data.mode === "newcomer" ? { understand: true } : {});
        setActivePanel("tests");
        setOutputs({
          tests: "Recovered active kata. Run tests to refresh this panel.",
          diff: "Recovered active kata. Open the diff when you want to inspect the practice copy.",
          hints: "Recovered active kata. Previously used hints are counted in the session header.",
          result: "No result yet.",
        });
        setErrors({});
        setResult(null);
        setForecastFile("");
        setCoachMessage("");
        setBeltPromotion(null);
        setMistakeIndex(null);
        await loadFiles(data.id);
      } catch {
        if (!cancelled) saveActiveSessionID("");
      } finally {
        if (!cancelled) setBusy("");
      }
    })();
    return () => {
      cancelled = true;
    };
  }, []);

  useEffect(() => {
    const id = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(id);
  }, []);

  useEffect(() => {
    if (senseiMode) {
      setPreflight(null);
      setPreflightError(null);
      return undefined;
    }
    if (!repo.trim()) {
      setPreflight(null);
      setPreflightError(null);
      return undefined;
    }
    if (remoteNeedsOpen) {
      setPreflight(null);
      setPreflightError(null);
      return undefined;
    }
    const id = window.setTimeout(async () => {
      await inspectRepo("/api/preflight", "preflight");
    }, 350);
    return () => window.clearTimeout(id);
  }, [repo, remoteNeedsOpen, senseiMode]);

  useEffect(() => {
    document.body.classList.toggle("is-busy", Boolean(busy));
  }, [busy]);

  useEffect(() => {
    const first = session?.task_files?.[0]?.path;
    if (!session || activeFile || !first) return;
    let cancelled = false;
    (async () => {
      try {
        const data = await api(`/api/sessions/${session.id}/files/${first}`);
        if (cancelled) return;
        setActiveFile(first);
        setContent(data.content || "");
      } catch {
        // Keep the empty editor state if the suggested file cannot be opened.
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [session, activeFile]);

  const markStep = useCallback((key, done = true) => {
    setWorkflow((current) => (current[key] === done ? current : { ...current, [key]: done }));
  }, []);

  const loadFiles = useCallback(
    async (id = session?.id) => {
      if (!id) return;
      const data = await api(`/api/sessions/${id}/files`);
      setFiles(data.files || []);
    },
    [session?.id],
  );

  async function inspectRepo(path, label) {
    setBusy(label);
    try {
      const data = await api(path, {
        method: "POST",
        body: JSON.stringify({ repo }),
      });
      setPreflight(data);
      setPreflightError(null);
      setOpenedRepo(repo.trim());
      if (data.review?.available) setMode("review");
      else if (data.learn?.available) setMode("learn");
    } catch (err) {
      setPreflight(null);
      setPreflightError(err);
    } finally {
      setBusy("");
    }
  }

  async function openRepo() {
    if (!repo.trim() || busy) return;
    await inspectRepo("/api/repos/open", "open");
  }

  async function startSession(event) {
    event.preventDefault();
    if (!canStart) return;
    setBusy("start");
    setErrors({});
    try {
      const endpoint = senseiMode ? "/api/sessions/sensei" : `/api/sessions/${mode}`;
      const body = senseiMode
        ? { pack_path: senseiPack.trim(), hint_budget: Number(hintBudget) }
        : {
            repo,
            difficulty: Number(difficulty),
            hint_budget: Number(hintBudget),
          };
      const data = await api(endpoint, {
        method: "POST",
        body: JSON.stringify(body),
      });
      if (!senseiMode) setRecentRepos((current) => rememberRecentRepo(repo, current));
      setSession(data);
      saveActiveSessionID(data.id);
      setWorkflow(data.mode === "newcomer" ? { understand: true } : {});
      setActivePanel("tests");
      setOutputs({
        tests: "No test run yet.",
        diff: "Open this panel after making edits or when you want to inspect the practice copy.",
        hints: "No hints used yet.",
        result: "No result yet.",
      });
      setResult(null);
      setForecastFile("");
      setCoachMessage("");
      setBeltPromotion(null);
      setMistakeIndex(null);
      await loadFiles(data.id);
    } catch (err) {
      setPreflightError(err);
    } finally {
      setBusy("");
    }
  }

  async function openFile(path) {
    if (!session || !path || path.endsWith("/")) return;
    setBusy("file");
    try {
      const data = await api(`/api/sessions/${session.id}/files/${path}`);
      setActiveFile(path);
      setContent(data.content || "");
      markStep("inspect");
    } catch (err) {
      setErrors((current) => ({ ...current, tests: err }));
    } finally {
      setBusy("");
    }
  }

  async function saveFile() {
    if (!session || !activeFile) return;
    setBusy("save");
    try {
      await api(`/api/sessions/${session.id}/files/${activeFile}`, {
        method: "PUT",
        body: JSON.stringify({ content }),
      });
      markStep("implement");
    } catch (err) {
      setErrors((current) => ({ ...current, tests: err }));
    } finally {
      setBusy("");
    }
  }

  async function runTests() {
    if (!session) return;
    setBusy("tests");
    setActivePanel("tests");
    try {
      const body = forecastFile ? JSON.stringify({ forecast_file: forecastFile }) : "{}";
      const data = await api(`/api/sessions/${session.id}/tests`, { method: "POST", body });
      const text = `${data.stdout || ""}${data.stderr || ""}\nexit code: ${data.exit_code}`;
      setOutputs((current) => ({ ...current, tests: text.trim() }));
      setErrors((current) => ({ ...current, tests: null }));
      setLatestTestExit(data.exit_code);
      markStep("tests");
    } catch (err) {
      setErrors((current) => ({ ...current, tests: err }));
    } finally {
      setBusy("");
    }
  }

  async function showDiff() {
    if (!session) return;
    setBusy("diff");
    setActivePanel("diff");
    try {
      const data = await api(`/api/sessions/${session.id}/diff`);
      setLatestDiff(data.diff || "");
      setOutputs((current) => ({ ...current, diff: data.diff || "(no local edits)" }));
      setErrors((current) => ({ ...current, diff: null }));
      markStep("diff");
    } catch (err) {
      setErrors((current) => ({ ...current, diff: err }));
    } finally {
      setBusy("");
    }
  }

  async function askHint() {
    if (!session) return;
    setBusy("hint");
    try {
      const data = await api(`/api/sessions/${session.id}/hints`, {
        method: "POST",
        body: JSON.stringify({ level: hintLevel }),
      });
      setOutputs((current) => ({
        ...current,
        hints: current.hints === "No hints used yet."
          ? `[Jin] ${data.hint}`
          : `${current.hints}\n\n[Jin] ${data.hint}`,
      }));
      setCoachMessage(data.hint || "");
      setErrors((current) => ({ ...current, hints: null }));
      if (data.cost != null) {
        setHintWallet((w) => w - data.cost);
      }
    } catch (err) {
      setErrors((current) => ({ ...current, hints: err }));
    } finally {
      setBusy("");
    }
  }

  async function submit() {
    if (!session) return;
    setBusy("submit");
    setActivePanel("result");
    try {
      const body =
        session.mode === "reviewer"
          ? {
              file_path: selectedFile || activeFile,
              start_line: Number(selectedStart || 1),
              end_line: Number(selectedEnd || selectedStart || 1),
              operator_class: reviewOperator,
              diagnosis: reviewDiagnosis,
              forecast_file: forecastFile,
            }
          : { forecast_file: forecastFile };
      const data = await api(`/api/sessions/${session.id}/submit`, {
        method: "POST",
        body: JSON.stringify(body),
      });
      setResult(data);
      setOutputs((current) => ({ ...current, result: renderResultText(data) }));
      markStep("submit");
      saveActiveSessionID("");
      if (data.mistake_index) setMistakeIndex(data.mistake_index);
      if (data.promotion) setBeltPromotion(data.promotion);
    } catch (err) {
      setErrors((current) => ({ ...current, result: err }));
    } finally {
      setBusy("");
    }
  }

  function selectReviewLine(line, shiftKey) {
    if (session?.mode !== "reviewer" || !activeFile) return;
    setSelectedFile(activeFile);
    if (shiftKey && selectedStart) {
      setSelectedEnd(line);
    } else {
      setSelectedStart(line);
      setSelectedEnd(line);
    }
    markStep("location");
  }

  function startAnother() {
    saveActiveSessionID("");
    setSession(null);
    setFiles([]);
    setActiveFile("");
    setContent("");
    setResult(null);
    setSelectedFile("");
    setSelectedStart(0);
    setSelectedEnd(0);
    setReviewDiagnosis("");
    setReviewOperator("");
    setWorkflow({});
    setForecastFile("");
    setCoachMessage("");
    setBeltPromotion(null);
    setMistakeIndex(null);
    setWrongFirstMode(false);
  }

  const suggestedFiles = session?.task_files || [];
  const next = checklist.find(([key]) => !workflow[key]);
  const modeLabel = session?.mode === "newcomer" ? "Learn" : "Review";
  const currentBelt = beltForDifficulty(session?.difficulty || difficulty);

  useEffect(() => {
    fetchKataHistory();
  }, [session]);

  async function fetchKataHistory() {
    try {
      const data = await api("/api/sessions");
      setKataHistory(data.sessions || []);
    } catch {
      // history fetch is non-critical
    }
  }

  async function browseRepos(path = "", showHidden = repoBrowserShowHidden) {
    setBusy("browse");
    try {
      const params = new URLSearchParams();
      if (path) params.set("path", path);
      if (showHidden) params.set("hidden", "1");
      const query = params.toString() ? `?${params}` : "";
      const data = await api(`/api/repos/browse${query}`);
      setRepoBrowser(data);
      setRepoBrowserError(null);
      setRepoBrowserOpen(true);
    } catch (err) {
      setRepoBrowserError(err);
    } finally {
      setBusy("");
    }
  }

  function toggleRepoBrowserHidden(showHidden) {
    setRepoBrowserShowHidden(showHidden);
    browseRepos(repoBrowser?.path || "", showHidden);
  }

  function chooseRepoPath(path) {
    setRepo(path);
    setRecentRepos((current) => rememberRecentRepo(path, current));
    setRepoBrowserOpen(false);
    setRepoBrowserError(null);
  }

  function forgetRecentRepo(path) {
    setRecentRepos((current) => {
      const next = current.filter((item) => item !== path);
      saveRecentRepos(next);
      return next;
    });
  }

  return (
    <main className="app">
      {beltPromotion && <div className="belt-promotion">{beltPromotion}</div>}
      {!session && (
        <section className="setup" id="setup">
          <div className="setup-hero">
            <div>
              <p className="eyebrow">CodeDojo local</p>
              <h1>Practice on a real repository</h1>
              <p className="hero-copy">Run a focused Learn or Review kata against a local practice copy, with tests, hints, and grading in one workspace.</p>
            </div>
            <div className="hero-signal" aria-hidden="true">
              <div className="signal-row">
                <span>repo scan</span>
                <strong id="hero-scan-state">{busy === "preflight" ? "scanning" : preflight ? preflight.language : "waiting"}</strong>
              </div>
              <div className="signal-row">
                <span>practice loop</span>
                <strong>inspect → test → submit</strong>
              </div>
              <div className="signal-bars">
                <span></span>
                <span></span>
                <span></span>
                <span></span>
              </div>
            </div>
          </div>

          <form id="start-form" className="panel setup-form" onSubmit={startSession}>
            <label>
              Repository
              <div className="repo-input-row">
                <input id="repo" name="repo" placeholder="/path/to/repo or git URL" required={!senseiMode} value={repo} onChange={(event) => setRepo(event.target.value)} />
                {remoteRepo && (
                  <button type="button" className="primary" onClick={openRepo} disabled={!repo.trim() || busy === "open"}>
                    {busy === "open" ? "Opening..." : "Open in CodeDojo"}
                  </button>
                )}
                <button type="button" className="secondary" onClick={() => browseRepos(repo && !remoteRepo ? repo : "")} disabled={busy === "browse"}>
                  {busy === "browse" ? "Browsing..." : "Browse"}
                </button>
              </div>
              {remoteRepo && remoteNeedsOpen && (
                <span className="repo-open-note">Server-side clone and inspection run only after opening the Git URL.</span>
              )}
            </label>
            <label>
              Sensei pack
              <input id="sensei-pack" name="sensei-pack" placeholder="/path/to/sensei-kata.json" value={senseiPack} onChange={(event) => setSenseiPack(event.target.value)} />
              {senseiMode && (
                <span className="repo-open-note">Starting uses this authored pack and skips automatic repository preflight.</span>
              )}
            </label>
            {repoBrowserOpen && (
              <RepoBrowser
                browser={repoBrowser}
                error={repoBrowserError}
                busy={busy === "browse"}
                showHidden={repoBrowserShowHidden}
                onBrowse={browseRepos}
                onToggleHidden={toggleRepoBrowserHidden}
                onSelect={chooseRepoPath}
                onClose={() => setRepoBrowserOpen(false)}
              />
            )}
            {recentRepos.length > 0 && (
              <RecentRepos
                repos={recentRepos}
                active={repo}
                onSelect={setRepo}
                onRemove={forgetRecentRepo}
              />
            )}
            <input id="mode" name="mode" type="hidden" value={mode} />
            <Preflight preflight={preflight} error={preflightError} />

            {!senseiMode && (
              <div className="mode-grid" aria-label="Session mode">
                <ModeCard name="review" selected={mode === "review"} availability={preflight?.review} recommended={preflight?.review?.available} language={preflight?.language} onSelect={setMode} />
                <ModeCard name="learn" selected={mode === "learn"} availability={preflight?.learn} recommended={!preflight?.review?.available && preflight?.learn?.available} language={preflight?.language} onSelect={setMode} />
              </div>
            )}

            <div className="setup-options">
              <label>
                Difficulty
                <div className="difficulty-belt" role="radiogroup" aria-label="Belt difficulty">
                  {BELTS.map((belt, i) => (
                    <button key={belt.name} type="button" data-belt={belt.name} aria-pressed={difficulty === i + 1} onClick={() => setDifficulty(i + 1)}>
                      {belt.label}
                    </button>
                  ))}
                </div>
              </label>
              <label>
                Hints
                <input id="hint-budget" name="hint-budget" type="number" min="0" max="10" value={hintBudget} onChange={(event) => setHintBudget(event.target.value)} />
              </label>
            </div>
            {mode === "review" && (
              <div className="setup-options">
                <label style={{ display: "flex", flexDirection: "row", alignItems: "center", gap: "8px", cursor: "pointer" }}>
                  <input type="checkbox" checked={wrongFirstMode} onChange={(e) => setWrongFirstMode(e.target.checked)} style={{ width: "auto" }} />
                  <span>Wrong-First Mode — predict the bug before opening source files</span>
                </label>
              </div>
            )}
            <button id="start-button" type="submit" disabled={!canStart}>
              {busy === "start" ? "Starting..." : senseiMode ? "Start Sensei kata" : "Start kata"}
            </button>
          </form>

          {kataHistory && kataHistory.length > 0 && (
            <section className="panel kata-library" style={{ marginTop: "24px", padding: "16px" }}>
              <h3 style={{ margin: "0 0 12px", fontSize: "18px" }}>Your Kata Scroll</h3>
              <div className="timeline-scroll">
                {kataHistory.slice(0, 10).map((entry) => (
                  <div key={entry.id} className={`timeline-entry ${entry.score > 0 ? "scored" : entry.state === "graded" ? "failed" : ""}`}>
                    <div className="entry-date">{new Date(entry.started_at).toLocaleDateString()}</div>
                    <div className="entry-type">{entry.mode} — {entry.task?.slice(0, 60)}{entry.task?.length > 60 ? "..." : ""}</div>
                    {entry.score > 0 && <div className="entry-score">Score: {entry.score}</div>}
                    {entry.operator && <div className="entry-verdict">{entry.operator}</div>}
                  </div>
                ))}
              </div>
            </section>
          )}
        </section>
      )}

      {session && (
        <section className="workspace" id="workspace">
          <header className="topbar">
            <div>
              <p className="eyebrow" id="mode-label">{modeLabel}</p>
              <h2 id="task-title">{session.task}</h2>
            </div>
            <div className="stats">
              <span className="belt-pill" data-belt={currentBelt.name}>
                <span className="belt-icon"></span>
                {currentBelt.label}
              </span>
              <span id="streak-label">Streak {session.streak}</span>
              <span id="hint-label">Hints {session.hints_used}/{session.hint_budget}</span>
              <span id="timer-label">{formatElapsed(session.started_at, now)}</span>
              <span id="progress-label">{checklist.length ? `${doneSteps}/${checklist.length} steps` : "0/0 steps"}</span>
            </div>
          </header>

          <div className="grid">
            <aside className="left-rail">
              <section className="panel guide">
                <div className="panel-head"><strong>Practice path</strong></div>
                <ol id="checklist" className="checklist">
                  {checklist.map(([key, label]) => <li key={key} className={workflow[key] ? "done" : ""}>{label}</li>)}
                </ol>
                <div id="next-action" className="next-action">
                  {next ? <><strong>Next</strong><span>{nextActionText[next[0]]}</span></> : "Kata workflow complete."}
                </div>
                <div className="suggested">
                  <strong id="suggested-title">Suggested files</strong>
                  <div id="suggested-files" className={`suggested-files ${suggestedFiles.length ? "" : "empty-state"}`}>
                    {suggestedFiles.length ? suggestedFiles.map((file) => (
                      <button key={file.path} className="suggested-file" type="button" disabled={wrongFirstMode && !workflow.tests} onClick={() => openFile(file.path)}>
                        <strong>{file.path}</strong>
                        <span>{file.reason}</span>
                      </button>
                    )) : "Start a kata to see suggested files."}
                  </div>
                </div>
              </section>

              {session?.mode === "reviewer" && (
                <section className="panel forecast">
                  <div className="forecast-section">
                    <h4>Forecast</h4>
                    <p style={{ color: "var(--muted)", fontSize: "13px", margin: 0 }}>{forecastFile ? `Predicted: ${forecastFile}` : "Where is the bug? Pick a file before running tests."}</p>
                    <div className="forecast-options">
                      {files.filter((f) => !f.dir).slice(0, 20).map((file) => (
                        <button key={file.path} type="button" className={`forecast-option ${forecastFile === file.path ? "selected" : ""}`} onClick={() => { setForecastFile(file.path); markStep("forecast"); }}>
                          {file.path}
                        </button>
                      ))}
                      {forecastFile && (
                        <button type="button" className="forecast-option" onClick={() => { setForecastFile(""); markStep("forecast", false); }}>No guess yet</button>
                      )}
                    </div>
                  </div>
                </section>
              )}

              <section className="panel files">
                <div className="panel-head">
                  <strong>Files</strong>
                  <button id="refresh-files" type="button" onClick={() => loadFiles()} disabled={Boolean(busy) || (wrongFirstMode && !workflow.tests)}>Refresh</button>
                </div>
                <div id="file-list" className={`file-list ${files.length ? "" : "empty-state"}`}>
                  {files.length ? files.filter((file) => !file.dir).map((file) => (
                    <button key={file.path} type="button" className={`file ${activeFile === file.path ? "active" : ""}`} onClick={() => openFile(file.path)} disabled={wrongFirstMode && !workflow.tests}>
                      {file.path}
                    </button>
                  )) : "Start a kata to load files."}
                </div>
              </section>
            </aside>

            <section className="panel editor">
              {wrongFirstMode && !workflow.tests && (
                <div className="wrong-first-lock fade-slide-up">
                  <h4>Wrong-First Mode</h4>
                  <p>You cannot open source files yet. Study the failing test output and predict the bug first. Run the tests above, then write your blind diagnosis below before unlocking the files.</p>
                  <div>
                    <textarea style={{ minHeight: "80px", marginBottom: "8px" }} placeholder="Blind diagnosis: what do you think the bug is?" value={reviewDiagnosis} onChange={(event) => { setReviewDiagnosis(event.target.value); if (event.target.value.trim()) markStep("diagnosis"); }}></textarea>
                    <button type="button" className="primary" onClick={() => markStep("tests")} disabled={!reviewDiagnosis.trim()}>I've written my blind diagnosis — unlock files</button>
                  </div>
                </div>
              )}
              <div className="panel-head">
                <strong id="active-file">{activeFile || "No file selected"}</strong>
                <button id="save-file" type="button" onClick={saveFile} disabled={Boolean(busy) || !activeFile}>Save</button>
              </div>
              <div className="editor-body">
                <LineNumbers content={content} selectedStart={selectedStart} selectedEnd={selectedEnd} onSelect={selectReviewLine} />
                <Editor
                  height="100%"
                  language={languageForPath(activeFile)}
                  value={content}
                  onChange={(value) => setContent(value || "")}
                  onMount={(editor) => { editorRef.current = editor; }}
                  options={{
                    minimap: { enabled: false },
                    fontSize: 13,
                    lineNumbers: "off",
                    folding: false,
                    wordWrap: "on",
                    scrollBeyondLastLine: false,
                    automaticLayout: true,
                    readOnly: !activeFile,
                  }}
                />
              </div>
            </section>

            <aside className="panel tools">
              <Tabs active={activePanel} setActive={setActivePanel} />
              <ToolPanel name="tests" active={activePanel} title="Tests" help="Run the repository checks inside the practice copy." action="Run tests" onAction={runTests} output={outputs.tests} error={errors.tests} busy={busy} />
              {!wrongFirstMode || workflow.tests ? (
                <ToolPanel name="diff" active={activePanel} title={session.mode === "newcomer" ? "Practice copy diff" : "Hidden bug diff"} help="Review the practice copy before submitting." action="Show diff" onAction={showDiff} output={outputs.diff} error={errors.diff} busy={busy} />
              ) : null}
              <section id="panel-hints" className={`tab-panel ${activePanel === "hints" ? "" : "hidden"}`} role="tabpanel" aria-labelledby="tab-hints">
                <div className="tool-head">
                  <div style={{ display: "flex", alignItems: "center", gap: "8px" }}>
                    <div className="coach-avatar" title="Jin — your coach">墨</div>
                    <div><h3>Hints</h3><p id="hint-help">Jin is your coach. Spend from the hint budget when you want guidance.</p></div>
                  </div>
                </div>
                {coachMessage && <div className="submission-summary coach-speak" style={{ borderColor: "var(--accent)" }}><em>"</em>{coachMessage}<em>"</em></div>}
                <div className="hint-actions">
                  <select id="hint-level" value={hintLevel} onChange={(event) => setHintLevel(event.target.value)}>
                    <option value="nudge">Nudge (cost: 1)</option>
                    <option value="question">Question (cost: 2)</option>
                    <option value="pointer">Pointer (cost: 4)</option>
                    <option value="concept">Concept (cost: 8)</option>
                  </select>
                  <button id="ask-hint" type="button" onClick={askHint} disabled={Boolean(busy)}>Ask</button>
                </div>
                <Output id="hint-output" text={outputs.hints} error={errors.hints} />
              </section>
              <SubmitPanel
                active={activePanel === "submit"}
                session={session}
                activeFile={activeFile}
                selectedFile={selectedFile}
                selectedStart={selectedStart}
                selectedEnd={selectedEnd}
                setSelectedFile={setSelectedFile}
                setSelectedStart={setSelectedStart}
                setSelectedEnd={setSelectedEnd}
                reviewOperator={reviewOperator}
                setReviewOperator={setReviewOperator}
                reviewDiagnosis={reviewDiagnosis}
                setReviewDiagnosis={(value) => { setReviewDiagnosis(value); if (value.trim()) markStep("diagnosis"); }}
                latestTestExit={latestTestExit}
                latestDiff={latestDiff}
                onSubmit={submit}
                busy={busy}
                forecastFile={forecastFile}
              />
              <section id="panel-result" className={`tab-panel result-panel ${activePanel === "result" ? "" : "hidden"}`} role="tabpanel" aria-labelledby="tab-result">
                <h3>Result</h3>
                <div id="result-summary" className={`result-summary ${result ? "" : "empty-state"}`}>
                  {result ? `Final score: ${result.score}` : "Submit a kata to see your score, feedback, and reveal."}
                </div>
                <ResultDetails result={result} />
                <Output id="result-output" text={outputs.result} error={errors.result} />
                {mistakeIndex && (
                  <div className="mistake-index fade-slide-up">
                    <h3>Your Mistake Index</h3>
                    {mistakeIndex.map((row) => (
                      <div key={row.operator} className="mistake-row">
                        <span className="op-name">{row.operator}</span>
                        <span className={`op-rate ${row.solve_rate < 0.5 ? "weak" : ""}`}>{Math.round(row.solve_rate * 100)}%</span>
                        <span className="op-time">{row.avg_minutes}m avg</span>
                        <span className="op-recommend">{row.recommended ? "← practice" : ""}</span>
                      </div>
                    ))}
                  </div>
                )}
                <button id="start-another" type="button" className="primary" onClick={startAnother}>Start another kata</button>
              </section>
            </aside>
          </div>
        </section>
      )}
    </main>
  );
}

function Preflight({ preflight, error }) {
  if (error) return <div id="preflight" className="preflight error-card" role="status" aria-live="polite"><strong>{error.title}</strong><span>{error.message}</span><ActionList actions={error.actions} /></div>;
  if (!preflight) return <div id="preflight" className="preflight" role="status" aria-live="polite">Enter a repository to scan language, tests, and mode availability.</div>;
  return (
    <div id="preflight" className="preflight" role="status" aria-live="polite">
      <strong>{preflight.repo_name || preflight.repo_path}</strong>
      <span>{preflight.language} · tests: {commandLabel(preflight.test_command)} · build: {commandLabel(preflight.build_command)}</span>
      <span>{availabilityText("Review", preflight.review)} · {availabilityText("Learn", preflight.learn)}</span>
    </div>
  );
}

function RepoBrowser({ browser, error, busy, showHidden, onBrowse, onToggleHidden, onSelect, onClose }) {
  const [jumpPath, setJumpPath] = useState(browser?.path || "");
  const breadcrumbs = browser?.path ? browserBreadcrumbs(browser.path) : [];

  useEffect(() => {
    setJumpPath(browser?.path || "");
  }, [browser?.path]);

  function jump() {
    const path = jumpPath.trim();
    if (path) onBrowse(path);
  }

  return (
    <div className="repo-browser" role="region" aria-label="Repository browser">
      <div className="repo-browser-head">
        <div>
          <strong>{browser?.path || "Choose a repository"}</strong>
          {browser?.is_repo && <span>Repository detected</span>}
        </div>
        <button type="button" className="icon-button" aria-label="Close repository browser" onClick={onClose}>×</button>
      </div>
      {error && <div className="error-card compact"><strong>{error.title}</strong><span>{error.message}</span></div>}
      {browser ? (
        <>
          <nav className="repo-browser-breadcrumbs" aria-label="Current folder path">
            {breadcrumbs.map((crumb, index) => (
              <React.Fragment key={crumb.path}>
                {index > 0 && <span aria-hidden="true">/</span>}
                <button type="button" onClick={() => onBrowse(crumb.path)} disabled={busy || crumb.path === browser.path}>
                  {crumb.label}
                </button>
              </React.Fragment>
            ))}
          </nav>
          <div className="repo-browser-jump">
            <input
              aria-label="Repository browser path"
              value={jumpPath}
              onChange={(event) => setJumpPath(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === "Enter") {
                  event.preventDefault();
                  jump();
                }
              }}
              disabled={busy}
            />
            <button type="button" onClick={jump} disabled={busy || !jumpPath.trim()}>Go</button>
          </div>
          <div className="repo-browser-actions">
            {browser.parent && <button type="button" onClick={() => onBrowse(browser.parent)} disabled={busy}>Up</button>}
            <label className="repo-browser-toggle">
              <input type="checkbox" checked={showHidden} onChange={(event) => onToggleHidden(event.target.checked)} disabled={busy} />
              <span>Show hidden</span>
            </label>
            <button type="button" className={browser.is_repo ? "primary" : "secondary"} onClick={() => onSelect(browser.path)} disabled={busy}>
              {browser.is_repo ? "Use this repository" : "Use this folder anyway"}
            </button>
          </div>
          {browser.truncated && <div className="repo-browser-note">Showing the first 500 matching folders.</div>}
          <div className={`repo-browser-list ${browser.entries?.length ? "" : "empty-state"}`}>
            {browser.entries?.length ? browser.entries.map((entry) => (
              <div key={entry.path} className={`repo-browser-entry ${entry.repo ? "repo" : ""} ${entry.symlink ? "symlink" : ""}`}>
                <button type="button" onClick={() => onBrowse(entry.path)} disabled={busy || entry.symlink}>
                  <strong>{entry.name}</strong>
                  <span>{entry.symlink ? "Symlinked folder" : entry.repo ? "Repository" : entry.hidden ? "Hidden folder" : "Folder"}</span>
                </button>
                {entry.repo && !entry.symlink && <button type="button" className="secondary" onClick={() => onSelect(entry.path)} disabled={busy}>Select</button>}
              </div>
            )) : "No child folders found."}
          </div>
        </>
      ) : (
        <button type="button" onClick={() => onBrowse("")} disabled={busy}>Open home folder</button>
      )}
    </div>
  );
}

function browserBreadcrumbs(path) {
  const normalized = path.replace(/[\/\\]+$/, "") || path;
  if (normalized === "/" || /^[A-Za-z]:[\/\\]?$/.test(normalized)) {
    return [{ label: normalized, path: normalized }];
  }
  if (/^[A-Za-z]:[\/\\]/.test(normalized)) {
    const drive = normalized.slice(0, 2);
    const parts = normalized.slice(3).split(/[\/\\]+/).filter(Boolean);
    let current = `${drive}\\`;
    return [
      { label: current, path: current },
      ...parts.map((part) => {
        current = current.endsWith("\\") ? `${current}${part}` : `${current}\\${part}`;
        return { label: part, path: current };
      }),
    ];
  }
  const absolute = normalized.startsWith("/");
  const parts = normalized.split("/").filter(Boolean);
  let current = absolute ? "/" : "";
  const crumbs = absolute ? [{ label: "/", path: "/" }] : [];
  for (const part of parts) {
    current = current === "/" ? `/${part}` : current ? `${current}/${part}` : part;
    crumbs.push({ label: part, path: current });
  }
  return crumbs;
}

function RecentRepos({ repos, active, onSelect, onRemove }) {
  return (
    <div className="recent-repos" aria-label="Recent repositories">
      {repos.map((path) => (
        <div key={path} className={`recent-repo ${path === active ? "active" : ""}`}>
          <button type="button" onClick={() => onSelect(path)}>
            <strong>{repoName(path)}</strong>
            <span>{path}</span>
          </button>
          <button type="button" className="icon-button" aria-label={`Remove ${path} from recent repositories`} onClick={() => onRemove(path)}>×</button>
        </div>
      ))}
    </div>
  );
}

function repoName(path) {
  const trimmed = path.replace(/[\/\\]+$/, "");
  const parts = trimmed.split(/[\/\\]/);
  return parts[parts.length - 1] || path;
}

function ModeCard({ name, selected, availability, recommended, language, onSelect }) {
  const review = name === "review";
  const disabled = availability && !availability.available;
  return (
    <button id={`${name}-card`} className={`mode-card ${selected ? "selected" : ""}`} type="button" data-mode={name} disabled={disabled} onClick={() => onSelect(name)}>
      <span className="mode-card-top">
        <strong>{review ? "Review" : "Learn"}</strong>
        <span id={`${name}-badge`} className={`mode-badge ${recommended ? "" : "hidden"}`}>Recommended</span>
      </span>
      <span className="mode-summary">{review ? "Find a hidden bug in the codebase." : "Rebuild a real historical change."}</span>
      <span className="mode-detail">{review ? "Locate the mutation, explain the diagnosis, and submit for grading." : "Implement the feature, run tests, and submit your implementation."}</span>
      <span className="mode-meta">{review ? "5-15 minutes" : "15-45 minutes"} · {language || "supported repositories"}</span>
      <span id={`${name}-status`} className="mode-status">{availabilityText(review ? "Review" : "Learn", availability)}</span>
    </button>
  );
}

function Tabs({ active, setActive }) {
  return (
    <div className="tabs" role="tablist" aria-label="Practice tools">
      {["tests", "diff", "hints", "submit", "result"].map((name) => (
        <button key={name} id={`tab-${name}`} className={`tab ${active === name ? "active" : ""}`} type="button" role="tab" aria-controls={`panel-${name}`} aria-selected={active === name ? "true" : "false"} data-panel={name} onClick={() => setActive(name)}>
          {name[0].toUpperCase() + name.slice(1)}
        </button>
      ))}
    </div>
  );
}

function ToolPanel({ name, active, title, help, action, onAction, output, error, busy }) {
  return (
    <section id={`panel-${name}`} className={`tab-panel ${active === name ? "" : "hidden"}`} role="tabpanel" aria-labelledby={`tab-${name}`}>
      <div className="tool-head">
        <div><h3>{title}</h3><p>{help}</p></div>
        <button id={name === "tests" ? "run-tests" : "show-diff"} type="button" onClick={onAction} disabled={Boolean(busy)}>{action}</button>
      </div>
      <Output id={`${name}-output`} text={output} error={error} />
    </section>
  );
}

function Output({ id, text, error }) {
  if (error) {
    return <pre id={id} className="error-output">{`${error.title || "Request failed"}\n${error.message}${error.actions?.length ? `\n\nTry this instead:\n${error.actions.map((action) => `- ${action}`).join("\n")}` : ""}`}</pre>;
  }
  const empty = !text || text.startsWith("No ") || text.startsWith("Open this panel");
  return <pre id={id} className={empty ? "empty-output" : ""}>{text}</pre>;
}

function LineNumbers({ content, selectedStart, selectedEnd, onSelect }) {
  const count = Math.max(1, content.split("\n").length);
  const low = Math.min(selectedStart || 0, selectedEnd || selectedStart || 0);
  const high = Math.max(selectedStart || 0, selectedEnd || selectedStart || 0);
  return (
    <div id="line-numbers" className="line-numbers" aria-hidden="true">
      {Array.from({ length: count }, (_, i) => {
        const line = i + 1;
        const selected = low && line >= low && line <= high;
        return <button key={line} type="button" className={selected ? "selected-line" : ""} onClick={(event) => onSelect(line, event.shiftKey)}>{line}</button>;
      })}
    </div>
  );
}

function SubmitPanel(props) {
  const {
    active, session, activeFile, selectedFile, selectedStart, selectedEnd, setSelectedFile, setSelectedStart, setSelectedEnd,
    reviewOperator, setReviewOperator, reviewDiagnosis, setReviewDiagnosis, latestTestExit, latestDiff, onSubmit, busy, forecastFile,
  } = props;
  return (
    <section id="panel-submit" className={`tab-panel submit ${active ? "" : "hidden"}`} role="tabpanel" aria-labelledby="tab-submit">
      <div id="review-submit" className={`review-submit ${session.mode === "reviewer" ? "" : "hidden"}`}>
        <h3>Hidden bug submission</h3>
        <p>Select the hidden bug location in the gutter, confirm the file/range, and explain what is wrong.</p>
        {forecastFile && <div className="submission-summary"><strong>Forecast:</strong> <span>{forecastFile}</span></div>}
        <div id="review-selection-summary" className={`submission-summary ${selectedFile ? "" : "empty-state"}`}>
          {selectedFile ? `${selectedFile}:${selectedStart || 1}-${selectedEnd || selectedStart || 1}` : "No hidden bug location selected yet."}
        </div>
        <div className="selection-actions">
          <button id="use-current-file" type="button" onClick={() => setSelectedFile(activeFile)}>Use current file</button>
          <button id="clear-selection" type="button" onClick={() => { setSelectedFile(""); setSelectedStart(0); setSelectedEnd(0); }}>Clear selection</button>
        </div>
        <input id="review-file" placeholder="file path" value={selectedFile} onChange={(event) => setSelectedFile(event.target.value)} />
        <div className="row">
          <input id="review-start" type="number" min="1" placeholder="start" value={selectedStart || ""} onChange={(event) => setSelectedStart(event.target.value)} />
          <input id="review-end" type="number" min="1" placeholder="end" value={selectedEnd || ""} onChange={(event) => setSelectedEnd(event.target.value)} />
        </div>
        <input id="review-operator" placeholder="issue type" value={reviewOperator} onChange={(event) => setReviewOperator(event.target.value)} />
        <textarea id="review-diagnosis" placeholder="diagnosis" value={reviewDiagnosis} onChange={(event) => setReviewDiagnosis(event.target.value)}></textarea>
      </div>
      <div id="learn-submit" className={`learn-submit ${session.mode === "newcomer" ? "" : "hidden"}`}>
        <h3>Reference solution submission</h3>
        <p>Submit your implementation after tests and diff review. The reference solution is revealed after grading.</p>
        {forecastFile && <div className="submission-summary"><strong>Forecast:</strong> <span>{forecastFile}</span></div>}
        <ul id="learn-presubmit" className="presubmit-checklist">
          <li className={latestTestExit !== null ? "done" : ""}>Tests run{latestTestExit !== null ? `, latest exit code ${latestTestExit}` : ""}</li>
          <li className={latestDiff ? "done" : ""}>Diff reviewed</li>
          <li>Hints optional</li>
        </ul>
      </div>
      <button id="submit" type="button" className="primary" onClick={onSubmit} disabled={Boolean(busy)}>Submit</button>
    </section>
  );
}

function ScoreCard({ result }) {
  if (!result) return null;
  const breakdown = result.breakdown || {};
  const entries = Object.entries(breakdown);
  const maxVal = Math.max(1, ...entries.map(([, v]) => Math.abs(v)));
  const verdict =
    result.score >= 90 ? "perfect" :
    result.score >= 60 ? "good" :
    "partial";
  const verdictLabel =
    result.score >= 90 ? "Exceptional" :
    result.score >= 60 ? "Solid work" :
    "Keep practicing";
  return (
    <div className="score-card fade-slide-up">
      <div className={`score-verdict ${verdict}`}>{verdictLabel}</div>
      <ul className="score-bar-list">
        {entries.map(([key, value]) => (
          <li key={key} className="score-bar-item">
            <span className="bar-label">{key}</span>
            <div className="bar-track"><div className="bar-fill ink-reveal" style={{ width: `${(Math.abs(value) / maxVal) * 100}%` }}></div></div>
            <span className="bar-value">{value > 0 ? `+${value}` : value}</span>
          </li>
        ))}
      </ul>
      {result.score >= 90 && <div className="score-stamp">Clean win</div>}
      {result.current_streak > 1 && <div style={{ fontSize: "13px", color: "var(--muted)" }}>Streak: {result.current_streak}</div>}
    </div>
  );
}

function ResultDetails({ result }) {
  if (!result) return <div id="result-details" className="result-details"></div>;
  return (
    <div id="result-details" className="result-details">
      <ScoreCard result={result} />
      {Object.entries(result.reveal || {}).length > 0 && (
        <div className="result-card ink-bleed">
          <h4>Reveal</h4>
          {Object.entries(result.reveal).map(([key, value]) => (
            <p key={key}><strong>{key}:</strong> {value}</p>
          ))}
        </div>
      )}
    </div>
  );
}

function ActionList({ actions = [] }) {
  return actions.length ? <ul>{actions.map((action) => <li key={action}>{action}</li>)}</ul> : null;
}

function renderResultText(result) {
  const breakdown = Object.entries(result.breakdown || {}).map(([key, value]) => `${key}: ${value}`).join("\n");
  const reveal = Object.entries(result.reveal || {}).map(([key, value]) => `${key}: ${value}`).join("\n");
  return [`score: ${result.score}`, breakdown, result.feedback, reveal && `reveal:\n${reveal}`].filter(Boolean).join("\n\n");
}

createRoot(document.getElementById("root")).render(<App />);
