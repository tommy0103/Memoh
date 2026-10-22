package textutil

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"
)

// StorageText makes binary bytes visible in text-only transports. PostgreSQL
// text and jsonb cannot represent NUL, even when JSON encodes it as an escape.
// Existing literal backslash sequences are left untouched.
func StorageText(text string) string {
	return strings.ReplaceAll(strings.ToValidUTF8(text, "\uFFFD"), "\x00", `\x00`)
}

// jsonbRejectedEscapes are the JSON escapes PostgreSQL refuses to store in
// jsonb: \u0000 (SQLSTATE 22P05) and anything in the UTF-16 surrogate
// range (SQLSTATE 22P02, raised for an unpaired surrogate). Both are
// syntactically valid JSON and pure ASCII, so neither json.Valid nor
// utf8.Valid rejects them - only the decode below repairs them, by mapping a
// lone surrogate to U+FFFD and folding a valid pair into real UTF-8.
//
// Every surrogate escape begins \ud or \uD, so screening that prefix also
// sends valid pairs down the slow path. That is deliberate: encoding/json
// emits astral characters as raw UTF-8 rather than escapes, so a surrogate
// pair in stored content is rare, while pairing them here would cost a scan
// on every message to save that rare case nothing.
var jsonbRejectedEscapes = [][]byte{
	[]byte(`\u0000`),
	[]byte(`\ud`),
	[]byte(`\uD`),
}

// jsonbSafeEscapes reports whether raw is free of escapes jsonb would reject.
func jsonbSafeEscapes(raw []byte) bool {
	for _, escape := range jsonbRejectedEscapes {
		if bytes.Contains(raw, escape) {
			return false
		}
	}
	return true
}

// StorageJSON normalizes string values before they reach jsonb. Decode only
// potentially unsafe input, preserving numeric precision and literal escapes.
func StorageJSON(raw []byte) ([]byte, error) {
	if !json.Valid(raw) {
		return nil, errors.New("invalid JSON content")
	}
	// Screening for a bare \u instead would push every message carrying an
	// ordinary escape down the decode path - encoding/json escapes <, > and &
	// by default, so any reply holding markup or a code block would pay a full
	// decode and re-encode on the persistence hot path.
	if utf8.Valid(raw) && jsonbSafeEscapes(raw) {
		return raw, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	value, err := storageJSONValue(value)
	if err != nil {
		return nil, err
	}
	return json.Marshal(value)
}

func storageJSONValue(value any) (any, error) {
	switch v := value.(type) {
	case string:
		return StorageText(v), nil
	case []any:
		for i, item := range v {
			clean, err := storageJSONValue(item)
			if err != nil {
				return nil, err
			}
			v[i] = clean
		}
	case map[string]any:
		clean := make(map[string]any, len(v))
		for key, item := range v {
			key = StorageText(key)
			if _, exists := clean[key]; exists {
				return nil, errors.New("JSON keys collide after text normalization")
			}
			next, err := storageJSONValue(item)
			if err != nil {
				return nil, err
			}
			clean[key] = next
		}
		return clean, nil
	}
	return value, nil
}
