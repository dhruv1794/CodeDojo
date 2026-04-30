package textop

import (
	"strings"
	"testing"
)

// --- Python boundary ---

func TestPythonBoundaryCandidate(t *testing.T) {
	src := "if x < limit:\n    pass\n"
	sites := PythonBoundary{}.Candidates(src)
	if len(sites) == 0 {
		t.Fatal("expected at least one candidate for '<'")
	}
	if sites[0].StartLine != 1 {
		t.Fatalf("start line = %d, want 1", sites[0].StartLine)
	}
}

func TestPythonBoundaryApply(t *testing.T) {
	cases := []struct {
		src  string
		op   string
		want string
	}{
		{"if x < limit:\n    pass\n", "<", "if x <= limit:\n    pass\n"},
		{"if x > 0:\n    pass\n", ">", "if x >= 0:\n    pass\n"},
	}
	for _, tc := range cases {
		sites := PythonBoundary{}.Candidates(tc.src)
		if len(sites) == 0 {
			t.Fatalf("no candidates for %q", tc.src)
		}
		got, err := PythonBoundary{}.Apply(tc.src, sites[0])
		if err != nil {
			t.Fatalf("Apply error: %v", err)
		}
		if got != tc.want {
			t.Fatalf("Apply(%q) = %q, want %q", tc.src, got, tc.want)
		}
	}
}

// --- Python conditional ---

func TestPythonConditionalInsertNot(t *testing.T) {
	src := "    if result:\n        return True\n"
	sites := PythonConditional{}.Candidates(src)
	if len(sites) == 0 {
		t.Fatal("expected candidate for simple if")
	}
	got, err := PythonConditional{}.Apply(src, sites[0])
	if err != nil {
		t.Fatalf("Apply error: %v", err)
	}
	if !strings.Contains(got, "if not result:") {
		t.Fatalf("expected 'if not result:', got: %q", got)
	}
}

func TestPythonConditionalRemoveNot(t *testing.T) {
	src := "    if not result:\n        return True\n"
	sites := PythonConditional{}.Candidates(src)
	if len(sites) == 0 {
		t.Fatal("expected candidate for 'if not'")
	}
	got, err := PythonConditional{}.Apply(src, sites[0])
	if err != nil {
		t.Fatalf("Apply error: %v", err)
	}
	if !strings.Contains(got, "if result:") || strings.Contains(got, "not result") {
		t.Fatalf("expected 'if result:', got: %q", got)
	}
}

// --- Python exception swallow ---

func TestPythonExceptSwallow(t *testing.T) {
	src := "try:\n    do_thing()\nexcept ValueError:\n    raise\n"
	sites := PythonExceptSwallow{}.Candidates(src)
	if len(sites) == 0 {
		t.Fatal("expected candidate for raise inside except")
	}
	got, err := PythonExceptSwallow{}.Apply(src, sites[0])
	if err != nil {
		t.Fatalf("Apply error: %v", err)
	}
	if strings.Contains(got, "raise") || !strings.Contains(got, "pass") {
		t.Fatalf("expected 'pass' replacing 'raise', got: %q", got)
	}
}

// --- Python slice bounds ---

func TestPythonSliceBoundsFromTo(t *testing.T) {
	src := "result = items[i:]\n"
	sites := PythonSliceBounds{}.Candidates(src)
	if len(sites) == 0 {
		t.Fatal("expected candidate for [i:]")
	}
	got, err := PythonSliceBounds{}.Apply(src, sites[0])
	if err != nil {
		t.Fatalf("Apply error: %v", err)
	}
	if !strings.Contains(got, "[:i]") {
		t.Fatalf("expected '[:i]', got: %q", got)
	}
}

// --- JS boundary ---

func TestJSBoundaryCandidate(t *testing.T) {
	src := "if (count < limit) {\n  break;\n}\n"
	sites := JSBoundary{}.Candidates(src)
	if len(sites) == 0 {
		t.Fatal("expected at least one candidate for '<'")
	}
}

func TestJSBoundaryApply(t *testing.T) {
	src := "if (count < limit) {\n  break;\n}\n"
	sites := JSBoundary{}.Candidates(src)
	if len(sites) == 0 {
		t.Fatalf("no candidates")
	}
	got, err := JSBoundary{}.Apply(src, sites[0])
	if err != nil {
		t.Fatalf("Apply error: %v", err)
	}
	if !strings.Contains(got, "count <= limit") {
		t.Fatalf("expected boundary flip, got: %q", got)
	}
}

// --- JS conditional ---

func TestJSConditionalInsertBang(t *testing.T) {
	src := "if (isValid) {\n  proceed();\n}\n"
	sites := JSConditional{}.Candidates(src)
	if len(sites) == 0 {
		t.Fatal("expected candidate for simple if")
	}
	got, err := JSConditional{}.Apply(src, sites[0])
	if err != nil {
		t.Fatalf("Apply error: %v", err)
	}
	if !strings.Contains(got, "if (!isValid)") {
		t.Fatalf("expected 'if (!isValid)', got: %q", got)
	}
}

func TestJSConditionalRemoveBang(t *testing.T) {
	src := "if (!isValid) {\n  proceed();\n}\n"
	sites := JSConditional{}.Candidates(src)
	if len(sites) == 0 {
		t.Fatal("expected candidate for 'if (!'")
	}
	got, err := JSConditional{}.Apply(src, sites[0])
	if err != nil {
		t.Fatalf("Apply error: %v", err)
	}
	if strings.Contains(got, "!isValid") || !strings.Contains(got, "if (isValid)") {
		t.Fatalf("expected '!isValid' removed, got: %q", got)
	}
}

// --- JS async error swallow ---

func TestJSAsyncErrorSwallow(t *testing.T) {
	src := "try {\n  await doThing();\n} catch (e) {\n  throw e;\n}\n"
	sites := JSAsyncErrorSwallow{}.Candidates(src)
	if len(sites) == 0 {
		t.Fatal("expected candidate for throw in catch")
	}
	got, err := JSAsyncErrorSwallow{}.Apply(src, sites[0])
	if err != nil {
		t.Fatalf("Apply error: %v", err)
	}
	if strings.Contains(got, "throw e") || !strings.Contains(got, "// error swallowed") {
		t.Fatalf("expected throw replaced, got: %q", got)
	}
}

// --- JS array bounds ---

func TestJSArrayBounds(t *testing.T) {
	src := "const val = arr[index];\n"
	sites := JSArrayBounds{}.Candidates(src)
	if len(sites) == 0 {
		t.Fatal("expected candidate for arr[index]")
	}
	got, err := JSArrayBounds{}.Apply(src, sites[0])
	if err != nil {
		t.Fatalf("Apply error: %v", err)
	}
	if !strings.Contains(got, "arr[index-1]") {
		t.Fatalf("expected off-by-one shift, got: %q", got)
	}
}

// --- TS optional chain ---

func TestTSOptionalChain(t *testing.T) {
	src := "const name = user?.profile.name;\n"
	sites := TSOptionalChain{}.Candidates(src)
	if len(sites) == 0 {
		t.Fatal("expected candidate for ?.")
	}
	got, err := TSOptionalChain{}.Apply(src, sites[0])
	if err != nil {
		t.Fatalf("Apply error: %v", err)
	}
	if strings.Contains(got, "?.") || !strings.Contains(got, "user.profile") {
		t.Fatalf("expected ?. removed, got: %q", got)
	}
}

// --- TS type guard weaken ---

func TestTSTypeGuardWeaken(t *testing.T) {
	src := "if (err instanceof NetworkError) {\n  retry();\n}\n"
	sites := TSTypeGuardWeaken{}.Candidates(src)
	if len(sites) == 0 {
		t.Fatal("expected candidate for instanceof")
	}
	got, err := TSTypeGuardWeaken{}.Apply(src, sites[0])
	if err != nil {
		t.Fatalf("Apply error: %v", err)
	}
	if strings.Contains(got, "instanceof") || !strings.Contains(got, "if (err)") {
		t.Fatalf("expected instanceof removed, got: %q", got)
	}
}

// --- Rust boundary ---

func TestRustBoundary(t *testing.T) {
	src := "if x < limit {\n    return;\n}\n"
	sites := RustBoundary{}.Candidates(src)
	if len(sites) == 0 {
		t.Fatal("expected candidate for '<'")
	}
	got, err := RustBoundary{}.Apply(src, sites[0])
	if err != nil {
		t.Fatalf("Apply error: %v", err)
	}
	if !strings.Contains(got, "x <= limit") {
		t.Fatalf("expected boundary flip, got: %q", got)
	}
}

// --- Rust option invert ---

func TestRustOptionInvertSomeToNone(t *testing.T) {
	src := "if result.is_some() {\n    use_it();\n}\n"
	sites := RustOptionInvert{}.Candidates(src)
	if len(sites) == 0 {
		t.Fatal("expected candidate for is_some()")
	}
	got, err := RustOptionInvert{}.Apply(src, sites[0])
	if err != nil {
		t.Fatalf("Apply error: %v", err)
	}
	if !strings.Contains(got, "is_none()") || strings.Contains(got, "is_some()") {
		t.Fatalf("expected is_some() → is_none(), got: %q", got)
	}
}

func TestRustOptionInvertIsOkToIsErr(t *testing.T) {
	src := "if result.is_ok() {\n    proceed();\n}\n"
	sites := RustOptionInvert{}.Candidates(src)
	if len(sites) == 0 {
		t.Fatal("expected candidate for is_ok()")
	}
	got, err := RustOptionInvert{}.Apply(src, sites[0])
	if err != nil {
		t.Fatalf("Apply error: %v", err)
	}
	if !strings.Contains(got, "is_err()") {
		t.Fatalf("expected is_ok() → is_err(), got: %q", got)
	}
}

// --- Rust range bound ---

func TestRustRangeBoundInclusiveToExclusive(t *testing.T) {
	src := "for i in 0..=n {\n    items.push(i);\n}\n"
	sites := RustRangeBound{}.Candidates(src)
	if len(sites) == 0 {
		t.Fatal("expected candidate for ..=")
	}
	got, err := RustRangeBound{}.Apply(src, sites[0])
	if err != nil {
		t.Fatalf("Apply error: %v", err)
	}
	if strings.Contains(got, "..=") || !strings.Contains(got, "0..n") {
		t.Fatalf("expected ..= → .., got: %q", got)
	}
}

// --- Rust err propagation ---

func TestRustErrPropagation(t *testing.T) {
	src := "let data = read_file(path)?\n;"
	sites := RustErrPropagation{}.Candidates(src)
	if len(sites) == 0 {
		t.Fatal("expected candidate for ?")
	}
	got, err := RustErrPropagation{}.Apply(src, sites[0])
	if err != nil {
		t.Fatalf("Apply error: %v", err)
	}
	if !strings.Contains(got, ".unwrap()") {
		t.Fatalf("expected ? → .unwrap(), got: %q", got)
	}
}

// --- AllXxx registries ---

func TestAllPythonRegistrySize(t *testing.T) {
	if len(AllPython()) != 4 {
		t.Fatalf("AllPython() len = %d, want 4", len(AllPython()))
	}
}

func TestAllJSRegistrySize(t *testing.T) {
	if len(AllJS()) != 4 {
		t.Fatalf("AllJS() len = %d, want 4", len(AllJS()))
	}
}

func TestAllTSRegistrySize(t *testing.T) {
	if len(AllTS()) != 6 {
		t.Fatalf("AllTS() len = %d, want 6", len(AllTS()))
	}
}

func TestAllRustRegistrySize(t *testing.T) {
	if len(AllRust()) != 4 {
		t.Fatalf("AllRust() len = %d, want 4", len(AllRust()))
	}
}
