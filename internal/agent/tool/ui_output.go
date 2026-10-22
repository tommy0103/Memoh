package tools

// UIOutputMetadataKey is a reserved top-level key in a tool's output map for
// payloads that exist only for the chat UI — e.g. the edit tool's context
// diff. The native runtime strips it before the SDK records the output, so
// the model never sees it: not in the current turn, not in rebuilt history.
// Everything a tool returns outside this key is model-visible; never put
// content the model should keep into it.
const UIOutputMetadataKey = "_ui"

// uiOutputKeys is the closed set of payload keys under UIOutputMetadataKey
// that the runtime forwards into UI metadata. Anything else under the key is
// still stripped from the model-facing output but never reaches the UI — an
// open passthrough would let any tool (including MCP-federated ones) inject
// or shadow reserved metadata such as execution_location.
var uiOutputKeys = map[string]struct{}{
	"diff": {},
}

// IsUIOutputKey reports whether key is a recognized UIOutputMetadataKey
// payload entry that may be forwarded into UI metadata.
func IsUIOutputKey(key string) bool {
	_, ok := uiOutputKeys[key]
	return ok
}
