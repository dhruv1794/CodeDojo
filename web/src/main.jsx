import React, { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { createRoot } from "react-dom/client";
import Editor from "@monaco-editor/react";
import "./styles.css";

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
    ["tests", "Run tests"],
    ["inspect", "Inspect suspicious files"],
    ["location", "Select bug location"],
    ["diagnosis", "Write diagnosis"],
    ["submit", "Submit review"],
  ],
};

const nextActionText = {
  understand: "Read the task brief, then inspect the files.",
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
  const editorRef = useRef(null);

  const sessionMode = session?.mode || "";
  const checklist = initialWorkflow[sessionMode] || [];
  const doneSteps = checklist.filter(([key]) => workflow[key]).length;
  const selectedAvailability = mode === "review" ? preflight?.review : preflight?.learn;
  const canStart = repo.trim() && !busy && (!preflight || selectedAvailability?.available);

  useEffect(() => {
    const fromCookie = cookie("codedojo_repo");
    if (fromCookie) setRepo(fromCookie);
  }, []);

  useEffect(() => {
    const id = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(id);
  }, []);

  useEffect(() => {
    if (!repo.trim()) {
      setPreflight(null);
      setPreflightError(null);
      return undefined;
    }
    const id = window.setTimeout(async () => {
      setBusy("preflight");
      try {
        const data = await api("/api/preflight", {
          method: "POST",
          body: JSON.stringify({ repo }),
        });
        setPreflight(data);
        setPreflightError(null);
        if (data.review?.available) setMode("review");
        else if (data.learn?.available) setMode("learn");
      } catch (err) {
        setPreflight(null);
        setPreflightError(err);
      } finally {
        setBusy("");
      }
    }, 350);
    return () => window.clearTimeout(id);
  }, [repo]);

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

  async function startSession(event) {
    event.preventDefault();
    if (!canStart) return;
    setBusy("start");
    setErrors({});
    try {
      const data = await api(`/api/sessions/${mode}`, {
        method: "POST",
        body: JSON.stringify({
          repo,
          difficulty: Number(difficulty),
          hint_budget: Number(hintBudget),
        }),
      });
      setSession(data);
      setWorkflow(data.mode === "newcomer" ? { understand: true } : {});
      setActivePanel("tests");
      setOutputs({
        tests: "No test run yet.",
        diff: "Open this panel after making edits or when you want to inspect the practice copy.",
        hints: "No hints used yet.",
        result: "No result yet.",
      });
      setResult(null);
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
      const data = await api(`/api/sessions/${session.id}/tests`, { method: "POST" });
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
        hints: current.hints === "No hints used yet." ? data.hint : `${current.hints}\n\n${data.hint}`,
      }));
      setErrors((current) => ({ ...current, hints: null }));
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
            }
          : {};
      const data = await api(`/api/sessions/${session.id}/submit`, {
        method: "POST",
        body: JSON.stringify(body),
      });
      setResult(data);
      setOutputs((current) => ({ ...current, result: renderResultText(data) }));
      markStep("submit");
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
  }

  const suggestedFiles = session?.task_files || [];
  const next = checklist.find(([key]) => !workflow[key]);
  const modeLabel = session?.mode === "newcomer" ? "Learn" : "Review";

  return (
    <main className="app">
      {!session && (
        <section className="setup" id="setup">
          <div className="setup-hero">
            <div>
              <p className="eyebrow">CodeDojo local</p>
              <h1>Practice on a real repository</h1>
              <p className="hero-copy">Run a focused Learn or Review session against a local practice copy, with tests, hints, and grading in one workspace.</p>
            </div>
            <div className="hero-signal" aria-hidden="true">
              <div className="signal-row">
                <span>repo scan</span>
                <strong id="hero-scan-state">{busy === "preflight" ? "scanning" : preflight ? preflight.language : "waiting"}</strong>
              </div>
              <div className="signal-row">
                <span>practice loop</span>
                <strong>inspect -&gt; test -&gt; submit</strong>
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
              <input id="repo" name="repo" placeholder="/path/to/repo or git URL" required value={repo} onChange={(event) => setRepo(event.target.value)} />
            </label>
            <input id="mode" name="mode" type="hidden" value={mode} />
            <Preflight preflight={preflight} error={preflightError} />

            <div className="mode-grid" aria-label="Session mode">
              <ModeCard name="review" selected={mode === "review"} availability={preflight?.review} recommended={preflight?.review?.available} language={preflight?.language} onSelect={setMode} />
              <ModeCard name="learn" selected={mode === "learn"} availability={preflight?.learn} recommended={!preflight?.review?.available && preflight?.learn?.available} language={preflight?.language} onSelect={setMode} />
            </div>

            <div className="setup-options">
              <label>
                Difficulty
                <input id="difficulty" name="difficulty" type="number" min="1" max="5" value={difficulty} onChange={(event) => setDifficulty(event.target.value)} />
              </label>
              <label>
                Hints
                <input id="hint-budget" name="hint-budget" type="number" min="0" max="10" value={hintBudget} onChange={(event) => setHintBudget(event.target.value)} />
              </label>
            </div>
            <button id="start-button" type="submit" disabled={!canStart}>
              {busy === "start" ? "Starting..." : "Start session"}
            </button>
          </form>
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
              <span id="difficulty-label">D{session.difficulty}</span>
              <span id="streak-label">Streak {session.streak}</span>
              <span id="hint-label">Hint budget {session.hints_used}/{session.hint_budget}</span>
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
                  {next ? <><strong>Next</strong><span>{nextActionText[next[0]]}</span></> : "Session workflow complete."}
                </div>
                <div className="suggested">
                  <strong id="suggested-title">Suggested files</strong>
                  <div id="suggested-files" className={`suggested-files ${suggestedFiles.length ? "" : "empty-state"}`}>
                    {suggestedFiles.length ? suggestedFiles.map((file) => (
                      <button key={file.path} className="suggested-file" type="button" onClick={() => openFile(file.path)}>
                        <strong>{file.path}</strong>
                        <span>{file.reason}</span>
                      </button>
                    )) : "Start a session to see suggested files."}
                  </div>
                </div>
              </section>

              <section className="panel files">
                <div className="panel-head">
                  <strong>Files</strong>
                  <button id="refresh-files" type="button" onClick={() => loadFiles()} disabled={Boolean(busy)}>Refresh</button>
                </div>
                <div id="file-list" className={`file-list ${files.length ? "" : "empty-state"}`}>
                  {files.length ? files.filter((file) => !file.dir).map((file) => (
                    <button key={file.path} type="button" className={`file ${activeFile === file.path ? "active" : ""}`} onClick={() => openFile(file.path)}>
                      {file.path}
                    </button>
                  )) : "Start a session to load files."}
                </div>
              </section>
            </aside>

            <section className="panel editor">
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
              <ToolPanel name="diff" active={activePanel} title={session.mode === "newcomer" ? "Practice copy diff" : "Hidden bug diff"} help="Review the practice copy before submitting." action="Show diff" onAction={showDiff} output={outputs.diff} error={errors.diff} busy={busy} />
              <section id="panel-hints" className={`tab-panel ${activePanel === "hints" ? "" : "hidden"}`} role="tabpanel" aria-labelledby="tab-hints">
                <div className="tool-head">
                  <div><h3>Hints</h3><p id="hint-help">Spend from the hint budget when you want a nudge.</p></div>
                </div>
                <div className="hint-actions">
                  <select id="hint-level" value={hintLevel} onChange={(event) => setHintLevel(event.target.value)}>
                    <option value="nudge">Nudge</option>
                    <option value="question">Question</option>
                    <option value="pointer">Pointer</option>
                    <option value="concept">Concept</option>
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
              />
              <section id="panel-result" className={`tab-panel result-panel ${activePanel === "result" ? "" : "hidden"}`} role="tabpanel" aria-labelledby="tab-result">
                <h3>Result</h3>
                <div id="result-summary" className={`result-summary ${result ? "" : "empty-state"}`}>
                  {result ? `Final score: ${result.score}` : "Submit a session to see your score, feedback, and reveal."}
                </div>
                <ResultDetails result={result} />
                <Output id="result-output" text={outputs.result} error={errors.result} />
                <button id="start-another" type="button" onClick={startAnother}>Start another session</button>
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

function ModeCard({ name, selected, availability, recommended, language, onSelect }) {
  const review = name === "review";
  const disabled = availability && !availability.available;
  return (
    <button id={`${name}-card`} className={`mode-card ${selected ? "selected" : ""}`} type="button" data-mode={name} disabled={disabled} onClick={() => onSelect(name)}>
      <span className="mode-card-top">
        <strong>{review ? "Review" : "Learn"}</strong>
        <span id={`${name}-badge`} className={`mode-badge ${recommended ? "" : "hidden"}`}>Recommended</span>
      </span>
      <span className="mode-summary">{review ? "For developers training AI-code review instincts." : "For developers learning a codebase through active recall."}</span>
      <span className="mode-detail">{review ? "Find the hidden bug, select its location, and explain the diagnosis." : "Rebuild a real historical change, run tests, and submit the implementation."}</span>
      <span id={`${name}-language`} className="mode-meta">{review ? "5-15 minutes" : "15-45 minutes"} · {language || "supported repositories"}</span>
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
    reviewOperator, setReviewOperator, reviewDiagnosis, setReviewDiagnosis, latestTestExit, latestDiff, onSubmit, busy,
  } = props;
  return (
    <section id="panel-submit" className={`tab-panel submit ${active ? "" : "hidden"}`} role="tabpanel" aria-labelledby="tab-submit">
      <div id="review-submit" className={`review-submit ${session.mode === "reviewer" ? "" : "hidden"}`}>
        <h3>Hidden bug submission</h3>
        <p>Select the hidden bug location in the gutter, confirm the file/range, and explain what is wrong.</p>
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

function ResultDetails({ result }) {
  if (!result) return <div id="result-details" className="result-details"></div>;
  return (
    <div id="result-details" className="result-details">
      {Object.entries(result.breakdown || {}).map(([key, value]) => <span key={key}>{key}: {value}</span>)}
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
