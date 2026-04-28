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
