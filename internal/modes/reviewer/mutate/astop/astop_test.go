// SPDX-License-Identifier: MIT

package astop

import (
	"strings"
	"testing"
)

func TestRustBoundary(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "lt to leq",
			src:  "fn clamp(x: i32) -> i32 {\n    if x < 5 {\n        return 5;\n    }\n    x\n}\n",
			want: "fn clamp(x: i32) -> i32 {\n    if x <= 5 {\n        return 5;\n    }\n    x\n}\n",
		},
		{
			name: "gt to geq",
			src:  "fn check(x: i32) -> bool {\n    x > 0\n}\n",
			want: "fn check(x: i32) -> bool {\n    x >= 0\n}\n",
		},
		{
			name: "leq to lt",
			src:  "fn clamp(x: i32) -> i32 {\n    if x <= 5 {\n        return 5;\n    }\n    x\n}\n",
			want: "fn clamp(x: i32) -> i32 {\n    if x < 5 {\n        return 5;\n    }\n    x\n}\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sites := RustBoundary{}.Candidates([]byte(tc.src))
			if len(sites) == 0 {
				t.Fatal("expected at least one candidate")
			}
			got, err := RustBoundary{}.Apply([]byte(tc.src), sites[0])
			if err != nil {
				t.Fatalf("Apply error: %v", err)
			}
			if string(got) != tc.want {
				t.Fatalf("Apply() =\n%q\nwant:\n%q", string(got), tc.want)
			}
		})
	}
}

func TestRustOptionInvert(t *testing.T) {
	cases := []struct {
		name string
		src  string
		has  string
		want string
	}{
		{
			name: "is_some to is_none",
			src:  "fn check(o: Option<i32>) -> bool {\n    o.is_some()\n}\n",
			has:  "is_some",
			want: "fn check(o: Option<i32>) -> bool {\n    o.is_none()\n}\n",
		},
		{
			name: "is_none to is_some",
			src:  "fn check(o: Option<i32>) -> bool {\n    o.is_none()\n}\n",
			has:  "is_none",
			want: "fn check(o: Option<i32>) -> bool {\n    o.is_some()\n}\n",
		},
		{
			name: "is_ok to is_err",
			src:  "fn check(r: Result<i32, E>) -> bool {\n    r.is_ok()\n}\n",
			has:  "is_ok",
			want: "fn check(r: Result<i32, E>) -> bool {\n    r.is_err()\n}\n",
		},
		{
			name: "is_err to is_ok",
			src:  "fn check(r: Result<i32, E>) -> bool {\n    r.is_err()\n}\n",
			has:  "is_err",
			want: "fn check(r: Result<i32, E>) -> bool {\n    r.is_ok()\n}\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sites := RustOptionInvert{}.Candidates([]byte(tc.src))
			if len(sites) == 0 {
				t.Fatal("expected at least one candidate")
			}
			got, err := RustOptionInvert{}.Apply([]byte(tc.src), sites[0])
			if err != nil {
				t.Fatalf("Apply error: %v", err)
			}
			if string(got) != tc.want {
				t.Fatalf("Apply() =\n%q\nwant:\n%q", string(got), tc.want)
			}
		})
	}
}

func TestRustRangeBound(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "inclusive to exclusive",
			src:  "fn main() {\n    for i in 0..=n {\n        println!(\"{}\", i);\n    }\n}\n",
			want: "fn main() {\n    for i in 0..n {\n        println!(\"{}\", i);\n    }\n}\n",
		},
		{
			name: "exclusive to inclusive",
			src:  "fn main() {\n    for i in 0..n {\n        println!(\"{}\", i);\n    }\n}\n",
			want: "fn main() {\n    for i in 0..=n {\n        println!(\"{}\", i);\n    }\n}\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sites := RustRangeBound{}.Candidates([]byte(tc.src))
			if len(sites) == 0 {
				t.Fatal("expected at least one candidate")
			}
			got, err := RustRangeBound{}.Apply([]byte(tc.src), sites[0])
			if err != nil {
				t.Fatalf("Apply error: %v", err)
			}
			if string(got) != tc.want {
				t.Fatalf("Apply() =\n%q\nwant:\n%q", string(got), tc.want)
			}
		})
	}
}

func TestRustErrPropagation(t *testing.T) {
	src := "fn read(path: &str) -> Result<String, Error> {\n    let data = std::fs::read_to_string(path)?;\n    Ok(data)\n}\n"
	want := "fn read(path: &str) -> Result<String, Error> {\n    let data = std::fs::read_to_string(path).unwrap();\n    Ok(data)\n}\n"

	sites := RustErrPropagation{}.Candidates([]byte(src))
	if len(sites) == 0 {
		t.Fatal("expected candidate for ?")
	}
	got, err := RustErrPropagation{}.Apply([]byte(src), sites[0])
	if err != nil {
		t.Fatalf("Apply error: %v", err)
	}
	if string(got) != want {
		t.Fatalf("Apply() =\n%q\nwant:\n%q", string(got), want)
	}
}

func TestJSBoundary(t *testing.T) {
	src := "if (x < 5) {\n  return true;\n}\n"
	sites := JSBoundary{}.Candidates([]byte(src))
	if len(sites) == 0 {
		t.Fatal("expected at least one candidate for <")
	}
	got, err := JSBoundary{}.Apply([]byte(src), sites[0])
	if err != nil {
		t.Fatalf("Apply error: %v", err)
	}
	if !strings.Contains(string(got), "x <= 5") {
		t.Fatalf("expected boundary flip to <=, got: %q", string(got))
	}
}

func TestJSBoundaryUsesAbstractReplacement(t *testing.T) {
	src := "if (x >= 5) {\n  return true;\n}\n"
	sites := JSBoundary{}.Candidates([]byte(src))
	if len(sites) == 0 {
		t.Fatal("expected at least one candidate for >=")
	}
	if sites[0].Metadata["from"] != ">=" || sites[0].Metadata["to"] != ">" {
		t.Fatalf("replacement metadata = %+v, want >= to >", sites[0].Metadata)
	}
	got, err := JSBoundary{}.Apply([]byte(src), sites[0])
	if err != nil {
		t.Fatalf("Apply error: %v", err)
	}
	if !strings.Contains(string(got), "x > 5") {
		t.Fatalf("expected boundary flip to >, got: %q", string(got))
	}
}

func TestJSStrictEquality(t *testing.T) {
	src := "if (user.id === input.id) {\n  return true;\n}\nif (kind !== expected) {\n  return false;\n}\n"
	sites := JSStrictEquality{}.Candidates([]byte(src))
	if len(sites) != 2 {
		t.Fatalf("Candidates len = %d, want 2", len(sites))
	}
	if sites[0].Metadata["from"] != "===" || sites[0].Metadata["to"] != "==" {
		t.Fatalf("first replacement metadata = %+v, want === to ==", sites[0].Metadata)
	}
	got, err := JSStrictEquality{}.Apply([]byte(src), sites[0])
	if err != nil {
		t.Fatalf("Apply error: %v", err)
	}
	if !strings.Contains(string(got), "user.id == input.id") || strings.Contains(string(got), "user.id === input.id") {
		t.Fatalf("expected strict equality weakened, got: %q", string(got))
	}
}

func TestJSConditionalInsertBang(t *testing.T) {
	src := "if (isValid) {\n  proceed();\n}\n"
	sites := JSConditional{}.Candidates([]byte(src))
	if len(sites) == 0 {
		t.Fatal("expected candidate for simple if")
	}
	got, err := JSConditional{}.Apply([]byte(src), sites[0])
	if err != nil {
		t.Fatalf("Apply error: %v", err)
	}
	if !strings.Contains(string(got), "if (!isValid)") {
		t.Fatalf("expected 'if (!isValid)', got: %q", string(got))
	}
}

func TestJSConditionalRemoveBang(t *testing.T) {
	src := "if (!isValid) {\n  proceed();\n}\n"
	sites := JSConditional{}.Candidates([]byte(src))
	if len(sites) == 0 {
		t.Fatal("expected candidate for 'if (!'")
	}
	got, err := JSConditional{}.Apply([]byte(src), sites[0])
	if err != nil {
		t.Fatalf("Apply error: %v", err)
	}
	if strings.Contains(string(got), "!isValid") || !strings.Contains(string(got), "if (isValid)") {
		t.Fatalf("expected '!isValid' removed, got: %q", string(got))
	}
}

func TestJSAsyncErrorSwallow(t *testing.T) {
	src := "try {\n  await doThing();\n} catch (e) {\n  throw e;\n}\n"
	sites := JSAsyncErrorSwallow{}.Candidates([]byte(src))
	if len(sites) == 0 {
		t.Fatal("expected candidate for throw in catch")
	}
	got, err := JSAsyncErrorSwallow{}.Apply([]byte(src), sites[0])
	if err != nil {
		t.Fatalf("Apply error: %v", err)
	}
	if strings.Contains(string(got), "throw e") || !strings.Contains(string(got), "// error swallowed") {
		t.Fatalf("expected throw replaced, got: %q", string(got))
	}
}

func TestJSArrayBounds(t *testing.T) {
	src := "const val = arr[index];\n"
	sites := JSArrayBounds{}.Candidates([]byte(src))
	if len(sites) == 0 {
		t.Fatal("expected candidate for arr[index]")
	}
	got, err := JSArrayBounds{}.Apply([]byte(src), sites[0])
	if err != nil {
		t.Fatalf("Apply error: %v", err)
	}
	if !strings.Contains(string(got), "arr[index-1]") {
		t.Fatalf("expected off-by-one shift, got: %q", string(got))
	}
}

func TestTSOptionalChain(t *testing.T) {
	src := "const name = user?.profile.name;\n"
	sites := TSOptionalChain{}.Candidates([]byte(src))
	if len(sites) == 0 {
		t.Fatal("expected candidate for ?.")
	}
	got, err := TSOptionalChain{}.Apply([]byte(src), sites[0])
	if err != nil {
		t.Fatalf("Apply error: %v", err)
	}
	if strings.Contains(string(got), "?.") || !strings.Contains(string(got), "user.profile") {
		t.Fatalf("expected ?. removed, got: %q", string(got))
	}
}

func TestTSTypeGuardWeaken(t *testing.T) {
	src := "if (err instanceof NetworkError) {\n  retry();\n}\n"
	sites := TSTypeGuardWeaken{}.Candidates([]byte(src))
	if len(sites) == 0 {
		t.Fatal("expected candidate for instanceof")
	}
	got, err := TSTypeGuardWeaken{}.Apply([]byte(src), sites[0])
	if err != nil {
		t.Fatalf("Apply error: %v", err)
	}
	if strings.Contains(string(got), "instanceof") || !strings.Contains(string(got), "if (err)") {
		t.Fatalf("expected instanceof removed, got: %q", string(got))
	}
}

func TestAllRustRegistrySize(t *testing.T) {
	if len(AllRust()) != 4 {
		t.Fatalf("AllRust() len = %d, want 4", len(AllRust()))
	}
}

func TestAllJSRegistrySize(t *testing.T) {
	if len(AllJS()) != 5 {
		t.Fatalf("AllJS() len = %d, want 5", len(AllJS()))
	}
}

func TestAllTSRegistrySize(t *testing.T) {
	if len(AllTS()) != 7 {
		t.Fatalf("AllTS() len = %d, want 7", len(AllTS()))
	}
}

func TestOperatorDefinitionsAreAbstractAndValid(t *testing.T) {
	if err := ensureOperatorDefinitions(BoundaryOperator, OptionPredicateOperator, RangeBoundOperator, StrictEqualityOperator); err != nil {
		t.Fatal(err)
	}
	if BoundaryOperator.Intent != "change relational operator at clamp/range check" {
		t.Fatalf("BoundaryOperator intent = %q", BoundaryOperator.Intent)
	}
	if got, ok := replacementFor(OptionPredicateOperator, "is_ok"); !ok || got != "is_err" {
		t.Fatalf("option replacement = %q, %v; want is_err, true", got, ok)
	}
	if (JSBoundary{}).Name() != languageOperatorName("js", BoundaryOperator) {
		t.Fatalf("JSBoundary name = %q, want language-scoped abstract operator name", (JSBoundary{}).Name())
	}
	if (JSStrictEquality{}).Name() != languageOperatorName("js", StrictEqualityOperator) {
		t.Fatalf("JSStrictEquality name = %q, want language-scoped abstract operator name", (JSStrictEquality{}).Name())
	}
}

func TestRustBoundaryNoFalseMatchGeneric(t *testing.T) {
	src := "fn parse<T: FromStr>(s: &str) -> Result<T, T::Err> {\n    s.parse()\n}\n"
	sites := RustBoundary{}.Candidates([]byte(src))
	for _, s := range sites {
		if s.StartLine > 0 {
			t.Fatalf("expected no candidates in generic params, got site at line %d: %s", s.StartLine, s.Description)
		}
	}
}

func TestJSBoundarySkipsComments(t *testing.T) {
	src := "// compare a < b\nif (a < b) {\n  return true;\n}\n"
	sites := JSBoundary{}.Candidates([]byte(src))
	if len(sites) == 0 {
		t.Fatal("expected at least one candidate for < in code")
	}
	if len(sites) > 2 {
		t.Fatalf("expected only non-comment candidates, got %d", len(sites))
	}
}
