package lottie

// The published @thorvg/webcanvas build minifies its export names:
// tvg_engine_init ships as "$b", malloc as "Zb". The names are stable within a
// ThorVG release and change between releases, so this table is recovered from
// the JavaScript glue that ships beside the module and must be regenerated
// whenever thorvg.wasm is upgraded. See doc.go for the procedure.
//
// Only the entry points this package actually drives are listed. A name that
// disappears in a future build fails loudly at load time rather than silently
// rendering nothing.
var exportNames = map[string]string{
	"malloc":                        "Zb",
	"free":                          "Yb",
	"tvg_engine_init":               "$b",
	"tvg_engine_term":               "ac",
	"tvg_swcanvas_create":           "bc",
	"tvg_swcanvas_set_target":       "fc",
	"tvg_canvas_destroy":            "ec",
	"tvg_canvas_add":                "gc",
	"tvg_canvas_update":             "jc",
	"tvg_canvas_draw":               "kc",
	"tvg_canvas_sync":               "lc",
	"tvg_animation_new":             "je",
	"tvg_animation_del":             "re",
	"tvg_animation_get_picture":     "ne",
	"tvg_animation_get_total_frame": "me",
	"tvg_animation_set_frame":       "ke",
	"tvg_picture_load_data":         "rd",
	"tvg_picture_set_size":          "td",
	"tvg_picture_get_size":          "ud",
}

// resizeHeapImport is the only host import ThorVG reaches on the render path:
// the hook malloc calls when linear memory is exhausted. It was identified by
// tracing every import through a full render; everything else traps.
const resizeHeapImport = "ub"

// ThorVG C API constants used here (thorvg_capi.h).
const (
	resultSuccess = 0
	// resultInsufficientCondition is returned by tvg_animation_set_frame when
	// the requested frame is already current. That is not a failure.
	resultInsufficientCondition = 2

	engineOptionDefault = 1
	// colorspaceABGR8888S is un-premultiplied ABGR. In little-endian memory
	// that is the byte order R,G,B,A — image.NRGBA's layout exactly, so a
	// frame is one copy with no per-pixel conversion.
	colorspaceABGR8888S = 2
)
