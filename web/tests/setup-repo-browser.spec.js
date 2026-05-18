import { expect, test } from "@playwright/test";

test("setup repo browser supports breadcrumbs, path jumps, hidden folders, and symlink labels", async ({ page }) => {
  const browseRequests = [];

  await page.route("**/api/sessions", async (route) => {
    await route.fulfill({ json: { sessions: [] } });
  });
  await page.route("**/api/preflight", async (route) => {
    const body = route.request().postDataJSON();
    await route.fulfill({
      json: {
        repo_path: body.repo,
        repo_name: body.repo.split(/[\\/]/).filter(Boolean).pop() || body.repo,
        language: "go",
        test_command: ["go", "test", "./..."],
        build_command: ["go", "build", "./..."],
        review: { available: true, candidate_count: 3 },
        learn: { available: false, reason: "no recent training commits" },
      },
    });
  });
  await page.route("**/api/repos/browse**", async (route) => {
    const url = new URL(route.request().url());
    const path = url.searchParams.get("path") || "/Users/dev";
    const hidden = url.searchParams.get("hidden") === "1";
    browseRequests.push({ path, hidden });
    const entries = [
      { name: "project", path: `${path}/project`, repo: true, hidden: false, symlink: false },
      { name: "linked-project", path: `${path}/linked-project`, repo: false, hidden: false, symlink: true },
    ];
    if (hidden) {
      entries.push({ name: ".archive", path: `${path}/.archive`, repo: false, hidden: true, symlink: false });
    }
    await route.fulfill({
      json: {
        path,
        parent: path === "/" ? "" : path.replace(/\/[^/]+$/, "") || "/",
        is_repo: path.endsWith("/project"),
        hidden,
        truncated: false,
        entries,
      },
    });
  });

  await page.goto("/");
  await page.getByRole("button", { name: "Browse" }).click();

  await expect(page.getByRole("region", { name: "Repository browser" })).toBeVisible();
  await expect(page.getByRole("navigation", { name: "Current folder path" })).toContainText("Users");
  await expect(page.getByRole("button", { name: /linked-project Symlinked folder/ })).toBeDisabled();

  await page.getByLabel("Repository browser path").fill("/workspace");
  await page.getByRole("button", { name: "Go" }).click();
  await expect(page.getByLabel("Repository browser path")).toHaveValue("/workspace");

  await page.getByLabel("Show hidden").check();
  await expect(page.getByText(".archive")).toBeVisible();

  await page.locator(".repo-browser-entry.repo").getByRole("button", { name: "Select" }).click();
  await expect(page.getByLabel("Repository")).toHaveValue("/workspace/project");
  await expect(page.getByRole("button", { name: "project /workspace/project" })).toBeVisible();

  expect(browseRequests).toContainEqual({ path: "/workspace", hidden: false });
  expect(browseRequests).toContainEqual({ path: "/workspace", hidden: true });
});

test("remote repository input opens explicitly before preflight", async ({ page }) => {
  const repoURL = "https://github.com/example/project.git";
  let preflightCalls = 0;
  let openCalls = 0;

  await page.route("**/api/sessions", async (route) => {
    await route.fulfill({ json: { sessions: [] } });
  });
  await page.route("**/api/preflight", async (route) => {
    preflightCalls += 1;
    await route.fulfill({ status: 500, json: { code: "unexpected", title: "Unexpected", message: "remote preflight should be explicit" } });
  });
  await page.route("**/api/repos/open", async (route) => {
    openCalls += 1;
    const body = route.request().postDataJSON();
    await route.fulfill({
      json: {
        repo_path: "/tmp/codedojo-clone/project",
        repo_name: "project",
        language: "javascript",
        test_command: ["npm", "test"],
        build_command: ["npm", "run", "build"],
        review: { available: true, candidate_count: 4 },
        learn: { available: false, reason: "no recent training commits" },
        requested_repo: body.repo,
      },
    });
  });

  await page.goto("/");
  await page.locator("#repo").fill(repoURL);

  await expect(page.getByRole("button", { name: "Open in CodeDojo" })).toBeVisible();
  await expect(page.getByText("Server-side clone and inspection run only after opening the Git URL.")).toBeVisible();
  await page.waitForTimeout(500);
  expect(preflightCalls).toBe(0);
  await expect(page.getByRole("button", { name: "Start kata" })).toBeDisabled();

  await page.getByRole("button", { name: "Open in CodeDojo" }).click();

  await expect(page.locator("#preflight")).toContainText("project");
  await expect(page.locator("#preflight")).toContainText("javascript");
  await expect(page.getByRole("button", { name: "Start kata" })).toBeEnabled();
  expect(openCalls).toBe(1);
});

test("sensei pack link starts authored kata without preflight", async ({ page }) => {
  let preflightCalls = 0;
  let senseiStartBody = null;
  const session = {
    id: "sensei-123",
    mode: "reviewer",
    repo: "/workspace/project",
    task: "A senior-authored cleanup kata.",
    task_files: [{ path: "calculator/calculator.go", reason: "Sensei-selected source file" }],
    difficulty: 2,
    hint_budget: 3,
    hints_used: 0,
    streak: 0,
    started_at: "2026-05-16T12:00:00Z",
    done: false,
  };

  await page.route("**/api/sessions", async (route) => {
    await route.fulfill({ json: { sessions: [] } });
  });
  await page.route("**/api/preflight", async (route) => {
    preflightCalls += 1;
    await route.fulfill({ status: 500, json: { code: "unexpected", title: "Unexpected", message: "sensei links should skip preflight" } });
  });
  await page.route("**/api/sessions/sensei", async (route) => {
    senseiStartBody = route.request().postDataJSON();
    await route.fulfill({ status: 201, json: session });
  });
  await page.route("**/api/sessions/sensei-123/files", async (route) => {
    await route.fulfill({ json: { files: [{ path: "calculator/calculator.go", dir: false }] } });
  });
  await page.route("**/api/sessions/sensei-123/files/calculator/calculator.go", async (route) => {
    await route.fulfill({ json: { content: "package calculator\n" } });
  });

  await page.goto("/?kata=%2Ftmp%2Fclamp-sensei-kata.json");

  await expect(page.locator("#sensei-pack")).toHaveValue("/tmp/clamp-sensei-kata.json");
  await expect(page.getByText("Starting uses this authored pack")).toBeVisible();
  await page.waitForTimeout(500);
  expect(preflightCalls).toBe(0);
  await expect(page.getByRole("button", { name: "Start Sensei kata" })).toBeEnabled();

  await page.getByRole("button", { name: "Start Sensei kata" }).click();

  expect(senseiStartBody).toMatchObject({ pack_path: "/tmp/clamp-sensei-kata.json", hint_budget: 3 });
  await expect(page.getByRole("heading", { name: "A senior-authored cleanup kata." })).toBeVisible();
});

test("active kata is recovered after page reload", async ({ page }) => {
  const session = {
    id: "kata-123",
    mode: "reviewer",
    repo: "/workspace/project",
    task: "Find the hidden boundary bug.",
    task_files: [{ path: "calculator/calculator.go", reason: "Recently changed logic" }],
    difficulty: 3,
    hint_budget: 3,
    hints_used: 1,
    streak: 2,
    started_at: "2026-05-16T12:00:00Z",
    done: false,
  };

  await page.route("**/api/sessions", async (route) => {
    if (route.request().method() === "POST") {
      await route.fulfill({ status: 201, json: session });
      return;
    }
    await route.fulfill({ json: { sessions: [] } });
  });
  await page.route("**/api/sessions/review", async (route) => {
    await route.fulfill({ status: 201, json: session });
  });
  await page.route("**/api/sessions/kata-123", async (route) => {
    await route.fulfill({ json: session });
  });
  await page.route("**/api/sessions/kata-123/files", async (route) => {
    await route.fulfill({
      json: {
        files: [
          { path: "calculator/calculator.go", dir: false },
          { path: "calculator/calculator_test.go", dir: false },
        ],
      },
    });
  });
  await page.route("**/api/sessions/kata-123/files/calculator/calculator.go", async (route) => {
    await route.fulfill({ json: { content: "package calculator\n\nfunc Add(a, b int) int { return a + b }\n" } });
  });
  await page.route("**/api/preflight", async (route) => {
    await route.fulfill({
      json: {
        repo_path: "/workspace/project",
        repo_name: "project",
        language: "go",
        test_command: ["go", "test", "./..."],
        build_command: ["go", "build", "./..."],
        review: { available: true, candidate_count: 3 },
        learn: { available: false, reason: "no recent training commits" },
      },
    });
  });

  await page.goto("/");
  await page.locator("#repo").fill("/workspace/project");
  await expect(page.getByRole("button", { name: "Start kata" })).toBeEnabled();
  await page.getByRole("button", { name: "Start kata" }).click();
  await expect(page.getByRole("heading", { name: "Find the hidden boundary bug." })).toBeVisible();
  await expect(page.locator("#hint-label")).toContainText("Hints 1/3");
  await expect(page.evaluate(() => window.localStorage.getItem("codedojo_active_session"))).resolves.toBe("kata-123");

  await page.reload();

  await expect(page.getByRole("heading", { name: "Find the hidden boundary bug." })).toBeVisible();
  await expect(page.locator("#hint-label")).toContainText("Hints 1/3");
  await expect(page.locator("#streak-label")).toContainText("Streak 2");
  await expect(page.locator("#active-file")).toContainText("calculator/calculator.go");
  await expect(page.locator("#tests-output")).toContainText("Recovered active kata");
});
