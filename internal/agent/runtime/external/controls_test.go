package external

import "testing"

// Native/model sessions have no external driver. Their control endpoint must
// keep returning an empty capability set rather than dereferencing a driver.
func TestReadControlsWithoutExternalDriver(t *testing.T) {
	controls, err := ReadControls(t.Context(), nil, PromptInput{ThreadID: "native-thread"})
	if err != nil || controls.SessionID != "native-thread" || controls.Capabilities.ForkLatestOnly || len(controls.Commands) != 0 {
		t.Fatalf("native controls=%+v error=%v", controls, err)
	}
}
