package config

import (
	"bytes"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	t.Run("normal key=value", func(t *testing.T) {
		in := "port=8080\nmock=false\n"
		got, err := Parse(strings.NewReader(in))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := map[string]string{"port": "8080", "mock": "false"}
		if len(got) != len(want) || got["port"] != "8080" || got["mock"] != "false" {
			t.Fatalf("got %v, want %v", got, want)
		}
	})

	t.Run("comments and blank lines skipped", func(t *testing.T) {
		in := "# a comment\n\nport=8080\n   \n# another\nmock=true\n"
		got, err := Parse(strings.NewReader(in))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 2 || got["port"] != "8080" || got["mock"] != "true" {
			t.Fatalf("got %v", got)
		}
	})

	t.Run("indented comment/blank still skipped after trim", func(t *testing.T) {
		in := "   # indented comment\n   \nport=1\n"
		got, err := Parse(strings.NewReader(in))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 1 || got["port"] != "1" {
			t.Fatalf("got %v", got)
		}
	})

	t.Run("equals sign inside value (path-like)", func(t *testing.T) {
		in := "db=/var/lib/pigate/pigate.db?opt=1\n"
		got, err := Parse(strings.NewReader(in))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got["db"] != "/var/lib/pigate/pigate.db?opt=1" {
			t.Fatalf("got %q", got["db"])
		}
	})

	t.Run("empty value is valid (tls-dir=)", func(t *testing.T) {
		in := "tls-dir=\n"
		got, err := Parse(strings.NewReader(in))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		val, ok := got["tls-dir"]
		if !ok || val != "" {
			t.Fatalf("got %q, ok=%v", val, ok)
		}
	})

	t.Run("line without = is an error", func(t *testing.T) {
		in := "port=8080\nthisisnotvalid\n"
		_, err := Parse(strings.NewReader(in))
		if err == nil {
			t.Fatalf("expected error, got nil")
		}
	})
}

func TestResolve(t *testing.T) {
	t.Run("defaults only, no file, no explicit", func(t *testing.T) {
		cfg, warns, err := Resolve(Defaults(), nil, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(warns) != 0 {
			t.Fatalf("unexpected warnings: %v", warns)
		}
		if cfg != Defaults() {
			t.Fatalf("got %+v, want defaults %+v", cfg, Defaults())
		}
	})

	t.Run("file overrides default", func(t *testing.T) {
		fileVals := map[string]string{"mock": "false", "port": "9000"}
		cfg, warns, err := Resolve(Defaults(), fileVals, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(warns) != 0 {
			t.Fatalf("unexpected warnings: %v", warns)
		}
		if cfg.Mock != false || cfg.Port != 9000 {
			t.Fatalf("got %+v", cfg)
		}
		// Untouched fields keep defaults.
		if cfg.DBPath != Defaults().DBPath {
			t.Fatalf("expected untouched DBPath to stay default, got %q", cfg.DBPath)
		}
	})

	t.Run("explicit flag overrides file", func(t *testing.T) {
		fileVals := map[string]string{"port": "9000"}
		explicit := map[string]string{"port": "1234"}
		cfg, _, err := Resolve(Defaults(), fileVals, explicit)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.Port != 1234 {
			t.Fatalf("got port=%d, want 1234", cfg.Port)
		}
	})

	t.Run("explicit flag wins even when file also sets it", func(t *testing.T) {
		fileVals := map[string]string{"mock": "false"}
		explicit := map[string]string{"mock": "true"}
		cfg, _, err := Resolve(Defaults(), fileVals, explicit)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.Mock != true {
			t.Fatalf("got mock=%v, want true (flag must win over file)", cfg.Mock)
		}
	})

	t.Run("unknown key produces a warning, not an error", func(t *testing.T) {
		fileVals := map[string]string{"totally-unknown-key": "1"}
		cfg, warns, err := Resolve(Defaults(), fileVals, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(warns) != 1 {
			t.Fatalf("expected 1 warning, got %v", warns)
		}
		if cfg != Defaults() {
			t.Fatalf("unknown key must not otherwise alter config, got %+v", cfg)
		}
	})

	t.Run("malformed int fails fast", func(t *testing.T) {
		fileVals := map[string]string{"port": "abc"}
		_, _, err := Resolve(Defaults(), fileVals, nil)
		if err == nil {
			t.Fatalf("expected error for port=abc, got nil")
		}
	})

	t.Run("malformed bool fails fast", func(t *testing.T) {
		fileVals := map[string]string{"mock": "x"}
		_, _, err := Resolve(Defaults(), fileVals, nil)
		if err == nil {
			t.Fatalf("expected error for mock=x, got nil")
		}
	})
}

func TestWriteParseRoundTrip(t *testing.T) {
	cfg := Defaults()
	cfg.Port = 9999
	cfg.Mock = false
	cfg.DBPath = "/var/lib/pigate/pigate.db"
	cfg.HTTPSPort = 443
	cfg.DockerCompat = true
	cfg.TLSDir = ""

	var buf bytes.Buffer
	if err := Write(&buf, cfg); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	fileVals, err := Parse(&buf)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	got, warns, err := Resolve(Defaults(), fileVals, nil)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if len(warns) != 0 {
		t.Fatalf("unexpected warnings from round-trip: %v", warns)
	}
	if got != cfg {
		t.Fatalf("round-trip mismatch:\n got  %+v\n want %+v", got, cfg)
	}
}

func TestWriteParseRoundTripDefaults(t *testing.T) {
	var buf bytes.Buffer
	if err := Write(&buf, Defaults()); err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	fileVals, err := Parse(&buf)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	got, _, err := Resolve(Defaults(), fileVals, nil)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if got != Defaults() {
		t.Fatalf("got %+v, want defaults %+v", got, Defaults())
	}
}

func TestKnownKeys(t *testing.T) {
	keys := KnownKeys()
	if len(keys) != 12 {
		t.Fatalf("expected 12 known keys, got %d: %v", len(keys), keys)
	}
	// "config" and "v" must never be treated as config-file keys.
	for _, k := range keys {
		if k == "config" || k == "v" {
			t.Fatalf("KnownKeys must not include %q", k)
		}
	}
}
