package suite

import "testing"

func TestParseOracleFull(t *testing.T) {
	o, err := ParseOracle("== stdout:\nhello\nworld\n== exit:\n3\n")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if o.Stdout != "hello\nworld" {
		t.Errorf("stdout = %q, want two lines", o.Stdout)
	}
	if o.Exit != 3 {
		t.Errorf("exit = %d, want 3", o.Exit)
	}
}

func TestParseOracleDefaults(t *testing.T) {
	// A missing exit section defaults to 0 and a missing stdout section to empty.
	o, err := ParseOracle("== stdout:\nout\n")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if o.Exit != 0 {
		t.Errorf("exit = %d, want default 0", o.Exit)
	}
}

func TestParseOracleException(t *testing.T) {
	o, err := ParseOracle("== stdout:\n\n== exception:\nRangeError\n== exit:\n1\n")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if o.Exception != "RangeError" {
		t.Errorf("exception = %q, want RangeError", o.Exception)
	}
}

func TestParseOracleErrors(t *testing.T) {
	if _, err := ParseOracle("== bogus:\nx\n"); err == nil {
		t.Error("an unknown section should be an error")
	}
	if _, err := ParseOracle("== exit:\nnotanumber\n"); err == nil {
		t.Error("a non-integer exit should be an error")
	}
}

func TestOracleRoundTrip(t *testing.T) {
	// Format then ParseOracle recovers the same values, so the offline generator's
	// output reads back into the runtime tier unchanged.
	want := Oracle{Stdout: "line one\nline two", Exception: "TypeError", Exit: 2}
	got, err := ParseOracle(want.Format())
	if err != nil {
		t.Fatalf("parse formatted: %v", err)
	}
	if got != want {
		t.Errorf("round trip = %+v, want %+v", got, want)
	}
}
