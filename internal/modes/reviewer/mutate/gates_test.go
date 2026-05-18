// SPDX-License-Identifier: MIT

package mutate

import "testing"

func TestCountTestResults(t *testing.T) {
	data := []byte(`{"Action":"run","Package":"example.com/sample","Test":"TestOne"}
{"Action":"pass","Package":"example.com/sample","Test":"TestOne","Elapsed":0}
{"Action":"run","Package":"example.com/sample","Test":"TestTwo"}
{"Action":"fail","Package":"example.com/sample","Test":"TestTwo","Elapsed":0}
{"Action":"pass","Package":"example.com/sample","Elapsed":0}
`)
	passed, failed, err := countTestResults(data)
	if err != nil {
		t.Fatalf("countTestResults returned error: %v", err)
	}
	if passed != 1 || failed != 1 {
		t.Fatalf("passed, failed = %d, %d; want 1, 1", passed, failed)
	}
}

func TestCountTestResultsRejectsMalformedJSON(t *testing.T) {
	if _, _, err := countTestResults([]byte("{")); err == nil {
		t.Fatal("countTestResults returned nil error for malformed JSON")
	}
}

func TestParsePytestSummaryVariants(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		output     string
		wantPassed int
		wantFailed int
	}{
		{
			name:       "passed only",
			output:     "======================== 5 passed in 0.04s ========================",
			wantPassed: 5,
			wantFailed: 0,
		},
		{
			name:       "failed passed skipped warnings",
			output:     "===== 2 failed, 5 passed, 1 skipped, 3 warnings in 0.04s =====",
			wantPassed: 5,
			wantFailed: 2,
		},
		{
			name:       "failed only",
			output:     "======================== 3 failed in 0.04s ========================",
			wantPassed: 0,
			wantFailed: 3,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			passed, failed, ok := parsePytestSummary(tt.output)
			if !ok {
				t.Fatal("parsePytestSummary() ok = false, want true")
			}
			if passed != tt.wantPassed || failed != tt.wantFailed {
				t.Fatalf("parsePytestSummary() = %d, %d; want %d, %d", passed, failed, tt.wantPassed, tt.wantFailed)
			}
		})
	}
}

func TestParseCargoSummaryVariants(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		output     string
		wantPassed int
		wantFailed int
	}{
		{
			name:       "ok",
			output:     "test result: ok. 7 passed; 0 failed; 0 ignored; 0 measured; 0 filtered out; finished in 0.01s",
			wantPassed: 7,
			wantFailed: 0,
		},
		{
			name:       "failed",
			output:     "test result: FAILED. 3 passed; 1 failed; 0 ignored; 0 measured; 0 filtered out; finished in 0.01s",
			wantPassed: 3,
			wantFailed: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			passed, failed, ok := parseCargoSummary(tt.output)
			if !ok {
				t.Fatal("parseCargoSummary() ok = false, want true")
			}
			if passed != tt.wantPassed || failed != tt.wantFailed {
				t.Fatalf("parseCargoSummary() = %d, %d; want %d, %d", passed, failed, tt.wantPassed, tt.wantFailed)
			}
		})
	}
}
