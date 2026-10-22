// Package lottie rasterises Lottie animations — Telegram animated stickers in
// particular — without cgo, a native library or a helper process.
//
// The renderer is the ThorVG vector engine's published WebAssembly build,
// driven through the pure-Go wazero runtime. That keeps the server binary
// CGO-free and, more importantly, keeps attacker-supplied animation data
// inside a sandbox: the module has no filesystem or network imports, its
// linear memory is capped, and a render that overruns its deadline is aborted
// rather than left to finish on its own.
package lottie

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"image"
	"io"
	"math"
)

const (
	// MaxJSONBytes bounds the decompressed animation. Telegram caps a .tgs at
	// 64 KiB compressed; this leaves generous room while keeping a gzip bomb
	// from being expanded in the first place.
	MaxJSONBytes = 2 * 1024 * 1024
	// MaxEdge is the longest edge of a rendered frame.
	MaxEdge = 512
	// MaxFrames is the most frames a single animation contributes.
	MaxFrames = 5
	// maxTotalFrames rejects animations whose timeline is implausibly long
	// before any of it is rasterised.
	maxTotalFrames = 7200
	// maxCanvasEdge bounds the composition size an animation may declare.
	maxCanvasEdge = 4096
)

// ErrNotAnimation reports input that is not a gzip-compressed animation.
var ErrNotAnimation = errors.New("not a gzip-compressed animation")

// IsTGS reports whether data carries the gzip magic that starts a Telegram
// animated sticker. The bytes decide: attachments routinely arrive labelled as
// images regardless of what they contain.
func IsTGS(data []byte) bool {
	return len(data) >= 2 && data[0] == 0x1f && data[1] == 0x8b
}

// Options bounds one render.
type Options struct {
	// Frames is the number of equidistant frames to rasterise, first and last
	// included. Defaults to MaxFrames and is clamped to it.
	Frames int
	// Edge is the longest edge of each frame. Defaults to MaxEdge, clamped to
	// it, and never enlarges an animation beyond its own canvas.
	Edge int
}

func (o Options) normalize() Options {
	if o.Frames <= 0 || o.Frames > MaxFrames {
		o.Frames = MaxFrames
	}
	if o.Edge <= 0 || o.Edge > MaxEdge {
		o.Edge = MaxEdge
	}
	return o
}

// RenderTGS decompresses a Telegram animated sticker and rasterises frames
// from it. The returned images are un-premultiplied NRGBA with the animation's
// own transparency intact; compositing is the caller's policy.
func RenderTGS(ctx context.Context, data []byte, opts Options) ([]*image.NRGBA, error) {
	if !IsTGS(data) {
		return nil, ErrNotAnimation
	}
	reader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, errors.New("animated sticker is not valid gzip")
	}
	defer func() { _ = reader.Close() }()
	// One byte past the limit distinguishes "exactly at the limit" from "more
	// than we are willing to expand".
	document, err := io.ReadAll(io.LimitReader(reader, MaxJSONBytes+1))
	if err != nil {
		return nil, errors.New("animated sticker could not be decompressed")
	}
	if len(document) == 0 || len(document) > MaxJSONBytes {
		return nil, fmt.Errorf("animated sticker is empty or exceeds %d bytes decompressed", MaxJSONBytes)
	}
	return Render(ctx, document, opts)
}

// Render rasterises frames from a raw Lottie JSON document.
func Render(ctx context.Context, document []byte, opts Options) ([]*image.NRGBA, error) {
	opts = opts.normalize()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	eng, err := loadEngine()
	if err != nil {
		return nil, err
	}
	in, err := eng.instantiate(ctx)
	if err != nil {
		return nil, err
	}
	defer in.close(context.WithoutCancel(ctx))
	return in.render(ctx, document, opts)
}

func (in *instance) render(ctx context.Context, document []byte, opts Options) ([]*image.NRGBA, error) {
	if err := in.check(ctx, "tvg_engine_init", nil, 1); err != nil {
		return nil, err
	}
	animation, err := in.call(ctx, "tvg_animation_new")
	if err != nil {
		return nil, err
	}
	if animation == 0 {
		return nil, errors.New("thorvg could not create an animation")
	}
	picture, err := in.call(ctx, "tvg_animation_get_picture", animation)
	if err != nil {
		return nil, err
	}

	documentPtr, err := in.alloc(ctx, document)
	if err != nil {
		return nil, err
	}
	mimePtr, err := in.alloc(ctx, []byte("lottie"))
	if err != nil {
		return nil, err
	}
	rpathPtr, err := in.alloc(ctx, nil)
	if err != nil {
		return nil, err
	}
	// copy=1: ThorVG keeps its own copy, so the guest allocation above is not
	// required to outlive the call.
	if err = in.check(ctx, "tvg_picture_load_data", nil,
		picture, uint64(documentPtr), uint64(len(document)), uint64(mimePtr), uint64(rpathPtr), 1); err != nil {
		return nil, errors.New("animated sticker could not be parsed as Lottie")
	}

	total, err := in.totalFrames(ctx, animation)
	if err != nil {
		return nil, err
	}
	if total <= 0 || total > maxTotalFrames {
		return nil, fmt.Errorf("animated sticker declares %d frames", total)
	}

	width, height, err := in.canvasSize(ctx, picture, opts.Edge)
	if err != nil {
		return nil, err
	}
	buffer, err := in.mallocGuest(ctx, uint64(width)*uint64(height)*4)
	if err != nil {
		return nil, err
	}
	canvas, err := in.call(ctx, "tvg_swcanvas_create", engineOptionDefault)
	if err != nil {
		return nil, err
	}
	if canvas == 0 {
		return nil, errors.New("thorvg could not create a canvas")
	}
	if err = in.check(ctx, "tvg_swcanvas_set_target", nil,
		canvas, uint64(buffer), uint64(width), uint64(width), uint64(height), colorspaceABGR8888S); err != nil {
		return nil, err
	}
	if err = in.check(ctx, "tvg_picture_set_size", nil, picture, f32(float32(width)), f32(float32(height))); err != nil {
		return nil, err
	}
	if err = in.check(ctx, "tvg_canvas_add", nil, canvas, picture); err != nil {
		return nil, err
	}

	frames := make([]*image.NRGBA, 0, opts.Frames)
	for _, index := range frameIndices(total, opts.Frames) {
		// tvg_animation_set_frame reports INSUFFICIENT_CONDITION when the
		// requested frame is already current, which is the normal answer for
		// frame 0 and for a single-frame animation.
		if err = in.check(ctx, "tvg_animation_set_frame", []uint64{resultInsufficientCondition},
			animation, f32(float32(index))); err != nil {
			return nil, err
		}
		if err = in.check(ctx, "tvg_canvas_update", nil, canvas); err != nil {
			return nil, err
		}
		if err = in.check(ctx, "tvg_canvas_draw", nil, canvas, 1); err != nil {
			return nil, err
		}
		if err = in.check(ctx, "tvg_canvas_sync", nil, canvas); err != nil {
			return nil, err
		}
		frame, err := in.readFrame(buffer, width, height)
		if err != nil {
			return nil, err
		}
		frames = append(frames, frame)
	}
	return frames, nil
}

func (in *instance) totalFrames(ctx context.Context, animation uint64) (int, error) {
	pointer, err := in.alloc(ctx, make([]byte, 4))
	if err != nil {
		return 0, err
	}
	if err = in.check(ctx, "tvg_animation_get_total_frame", nil, animation, uint64(pointer)); err != nil {
		return 0, err
	}
	raw, ok := in.memory.ReadUint32Le(pointer)
	if !ok {
		return 0, errors.New("thorvg frame count out of range")
	}
	count := math.Float32frombits(raw)
	if math.IsNaN(float64(count)) || math.IsInf(float64(count), 0) || count < 0 || count > maxTotalFrames {
		return 0, errors.New("thorvg reported an implausible frame count")
	}
	return int(count), nil
}

// canvasSize takes the animation's own composition size and fits it inside the
// edge budget without enlarging it.
func (in *instance) canvasSize(ctx context.Context, picture uint64, edge int) (uint32, uint32, error) {
	pointer, err := in.alloc(ctx, make([]byte, 8))
	if err != nil {
		return 0, 0, err
	}
	if err = in.check(ctx, "tvg_picture_get_size", nil, picture, uint64(pointer), uint64(pointer+4)); err != nil {
		return 0, 0, err
	}
	rawW, okW := in.memory.ReadUint32Le(pointer)
	rawH, okH := in.memory.ReadUint32Le(pointer + 4)
	if !okW || !okH {
		return 0, 0, errors.New("thorvg canvas size out of range")
	}
	width := float64(math.Float32frombits(rawW))
	height := float64(math.Float32frombits(rawH))
	if !(width >= 1) || !(height >= 1) || width > maxCanvasEdge || height > maxCanvasEdge {
		return 0, 0, fmt.Errorf("animated sticker declares a %.0fx%.0f canvas", width, height)
	}
	scale := math.Min(1, math.Min(float64(edge)/width, float64(edge)/height))
	return fitEdge(width * scale), fitEdge(height * scale), nil
}

// fitEdge clamps a scaled edge into the canvas range, so the result is always
// between 1 and maxCanvasEdge.
func fitEdge(v float64) uint32 {
	switch {
	case !(v >= 1):
		return 1
	case v > maxCanvasEdge:
		return maxCanvasEdge
	}
	return uint32(v) //nolint:gosec // G115: clamped to [1, maxCanvasEdge] immediately above.
}

// readFrame copies the canvas out of guest memory. ThorVG writes ABGR8888S,
// which in little-endian memory is the byte order R,G,B,A — image.NRGBA's
// layout — so the frame needs no per-pixel conversion.
func (in *instance) readFrame(buffer, width, height uint32) (*image.NRGBA, error) {
	pixels, ok := in.memory.Read(buffer, width*height*4)
	if !ok {
		return nil, errors.New("thorvg canvas read out of range")
	}
	frame := image.NewNRGBA(image.Rect(0, 0, int(width), int(height)))
	copy(frame.Pix, pixels)
	return frame, nil
}

// frameIndices picks count equidistant frames across a total-frame timeline,
// always including the first and the last.
func frameIndices(total, count int) []int {
	if total <= 0 || count <= 0 {
		return nil
	}
	if total <= count {
		indices := make([]int, total)
		for i := range indices {
			indices[i] = i
		}
		return indices
	}
	indices := make([]int, count)
	for i := range indices {
		indices[i] = int(math.Round(float64(i) * float64(total-1) / float64(count-1)))
	}
	return indices
}

func f32(v float32) uint64 { return uint64(math.Float32bits(v)) }
