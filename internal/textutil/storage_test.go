package textutil

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestStorageJSONPreservesEscapesAndNormalizesBinaryText(t *testing.T) {
	raw := []byte(`{"nested":["a\u0000b","\\u0000","\ud800","\ud83d\ude00"],"number":9007199254740993}`)
	clean, err := StorageJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Nested []string    `json:"nested"`
		Number json.Number `json:"number"`
	}
	if err := json.Unmarshal(clean, &decoded); err != nil {
		t.Fatal(err)
	}
	want := []string{`a\x00b`, `\u0000`, "�", "😀"}
	for i, v := range want {
		if decoded.Nested[i] != v {
			t.Fatalf("value %d = %q, want %q", i, decoded.Nested[i], v)
		}
	}
	if decoded.Number.String() != "9007199254740993" {
		t.Fatal("lost numeric precision")
	}
	again, err := StorageJSON(clean)
	if err != nil || string(again) != string(clean) {
		t.Fatalf("not idempotent: %s %v", again, err)
	}
}

func TestStorageJSONRejectsAmbiguousKeysAndInvalidDocuments(t *testing.T) {
	for _, raw := range []string{`{"a\u0000":1,"a\\x00":2}`, `{} {}`, `{"x":`} {
		if _, err := StorageJSON([]byte(raw)); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
}

// encoding/json escapes <, > and & by default, so ordinary replies carry
// escapes constantly. Those documents must not pay a decode and re-encode:
// the returned slice has to be the caller's own backing array.
func TestStorageJSONReturnsInputUntouchedWithoutNUL(t *testing.T) {
	raw := []byte(`{"text":"\u003cdiv\u003e a \u0026 b","emoji":"😀"}`)
	clean, err := StorageJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(clean) == 0 || len(raw) == 0 || &clean[0] != &raw[0] {
		t.Fatalf("re-encoded a document that holds no NUL escape: %s", clean)
	}
}

// A lone surrogate escape is pure ASCII and syntactically valid JSON, so it
// slips past both json.Valid and utf8.Valid. jsonb still rejects it with
// SQLSTATE 22P02, so it must not take the fast path. The assertion is on the
// returned bytes, not on a decode of them: encoding/json repairs a lone
// surrogate on the way into a Go string, so decoding would pass even when the
// escape survived untouched and reached the database. Deliberately carries no
// NUL escape, which would otherwise mask a fast path screening only for NUL.
func TestStorageJSONRepairsSurrogateEscapesWithoutNUL(t *testing.T) {
	for name, raw := range map[string]string{
		"lone high":  `{"role":"user","content":"\ud800"}`,
		"lone low":   `{"role":"user","content":"\udc00"}`,
		"uppercase":  `{"role":"user","content":"\uD800"}`,
		"valid pair": `{"role":"user","content":"\ud83d\ude00"}`,
	} {
		t.Run(name, func(t *testing.T) {
			clean, err := StorageJSON([]byte(raw))
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(clean, []byte(`\ud`)) || bytes.Contains(clean, []byte(`\uD`)) {
				t.Fatalf("surrogate escape reached jsonb: %s", clean)
			}
			if !json.Valid(clean) {
				t.Fatalf("repair produced invalid JSON: %s", clean)
			}
		})
	}
}
