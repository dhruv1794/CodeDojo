package calculator

import "testing"

func TestDivide(t *testing.T) {
	got, err := Divide(12, 3)
	if err != nil {
		t.Fatalf("Divide returned error: %v", err)
	}
	if got != 4 {
		t.Fatalf("Divide() = %d, want 4", got)
	}
}

func TestDivideByZero(t *testing.T) {
	if _, err := Divide(12, 0); err == nil {
		t.Fatal("Divide returned nil error for zero parts")
	}
}

func TestClamp(t *testing.T) {
	cases := []struct {
		name  string
		value int
		min   int
		max   int
		want  int
	}{
		{name: "low", value: -1, min: 0, max: 10, want: 0},
		{name: "mid", value: 5, min: 0, max: 10, want: 5},
		{name: "high", value: 11, min: 0, max: 10, want: 10},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Clamp(tc.value, tc.min, tc.max); got != tc.want {
				t.Fatalf("Clamp() = %d, want %d", got, tc.want)
			}
		})
	}
}
