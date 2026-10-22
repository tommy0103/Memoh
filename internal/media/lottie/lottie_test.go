package lottie

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"image"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"
)

// movingSquare is a red 128x128 square travelling across a 512x512 canvas over
// two seconds. Its position is a known function of time, so a render can be
// checked against arithmetic rather than a golden image.
const movingSquare = `{"v":"5.5.2","w":512,"h":512,"ip":0,"op":60,"fr":30,"layers":[{"ty":1,"ind":1,"ip":0,"op":60,"st":0,"sw":128,"sh":128,"sc":"#ff0000","ks":{"o":{"a":0,"k":100},"r":{"a":0,"k":0},"p":{"a":1,"k":[{"t":0,"s":[0,128,0],"e":[384,128,0]},{"t":59,"s":[384,128,0]}]},"a":{"a":0,"k":[0,0,0]},"s":{"a":0,"k":[100,100,100]}}}]}`

func tgs(t *testing.T, document string) []byte {
	t.Helper()
	var buf bytes.Buffer
	writer := gzip.NewWriter(&buf)
	if _, err := writer.Write([]byte(document)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// redCentroidX returns the mean x of the frame's red pixels, or -1 when there
// are none.
func redCentroidX(t *testing.T, frame *image.NRGBA) int {
	t.Helper()
	sum, count := 0, 0
	bounds := frame.Bounds()
	for y := range bounds.Dy() {
		for x := range bounds.Dx() {
			c := frame.NRGBAAt(x, y)
			if c.A > 200 && c.R > 200 && c.G < 60 && c.B < 60 {
				sum += x
				count++
			}
		}
	}
	if count == 0 {
		return -1
	}
	return sum / count
}

func TestRenderTGSProducesMovingFrames(t *testing.T) {
	frames, err := RenderTGS(context.Background(), tgs(t, movingSquare), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != MaxFrames {
		t.Fatalf("RenderTGS() = %d frames, want %d", len(frames), MaxFrames)
	}
	var centroids []int
	for i, frame := range frames {
		if frame.Bounds().Dx() != 512 || frame.Bounds().Dy() != 512 {
			t.Fatalf("frame %d is %v, want 512x512", i, frame.Bounds())
		}
		x := redCentroidX(t, frame)
		if x < 0 {
			t.Fatalf("frame %d has no red square", i)
		}
		centroids = append(centroids, x)
	}
	// The square starts at x=0 and ends at x=384, so its 128-wide centre runs
	// 64 -> 448. Sampling first and last frame must show that whole span.
	if centroids[0] > 80 || centroids[len(centroids)-1] < 430 {
		t.Fatalf("centroids = %v, want a sweep from ~64 to ~448", centroids)
	}
	for i := 1; i < len(centroids); i++ {
		if centroids[i] <= centroids[i-1] {
			t.Fatalf("centroids = %v, want strictly increasing — frames are not distinct", centroids)
		}
	}
}

func TestRenderTGSKeepsTransparency(t *testing.T) {
	frames, err := RenderTGS(context.Background(), tgs(t, movingSquare), Options{Frames: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 1 {
		t.Fatalf("RenderTGS() = %d frames, want 1", len(frames))
	}
	// The corner is outside the square: the animation's own transparency has
	// to survive, so the caller can decide what to composite it on.
	if corner := frames[0].NRGBAAt(511, 0); corner.A != 0 {
		t.Fatalf("corner pixel = %+v, want fully transparent", corner)
	}
}

func TestRenderTGSFitsWithinEdgeWithoutEnlarging(t *testing.T) {
	frames, err := RenderTGS(context.Background(), tgs(t, movingSquare), Options{Frames: 1, Edge: 128})
	if err != nil {
		t.Fatal(err)
	}
	if got := frames[0].Bounds(); got.Dx() != 128 || got.Dy() != 128 {
		t.Fatalf("bounds = %v, want 128x128", got)
	}

	small := `{"v":"5.5.2","w":64,"h":64,"ip":0,"op":2,"fr":30,"layers":[]}`
	frames, err = RenderTGS(context.Background(), tgs(t, small), Options{Frames: 1})
	if err != nil {
		t.Fatal(err)
	}
	if got := frames[0].Bounds(); got.Dx() != 64 || got.Dy() != 64 {
		t.Fatalf("bounds = %v, want the animation's own 64x64 — never enlarged", got)
	}
}

func TestRenderTGSRejectsBadInput(t *testing.T) {
	ctx := context.Background()
	if _, err := RenderTGS(ctx, []byte("not gzip at all"), Options{}); !errors.Is(err, ErrNotAnimation) {
		t.Fatalf("RenderTGS(plain bytes) error = %v, want ErrNotAnimation", err)
	}
	if _, err := RenderTGS(ctx, []byte{0x1f, 0x8b, 0x00, 0x01}, Options{}); err == nil {
		t.Fatal("RenderTGS(gzip magic without a stream) = nil error")
	}
	if _, err := RenderTGS(ctx, tgs(t, `{"not":"lottie"}`), Options{}); err == nil {
		t.Fatal("RenderTGS(non-Lottie JSON) = nil error")
	}
	if _, err := RenderTGS(ctx, tgs(t, ""), Options{}); err == nil {
		t.Fatal("RenderTGS(empty document) = nil error")
	}
}

// A gzip bomb must be refused while it is still compressed.
func TestRenderTGSRefusesOversizedDocument(t *testing.T) {
	var buf bytes.Buffer
	writer := gzip.NewWriter(&buf)
	if _, err := writer.Write(bytes.Repeat([]byte(" "), MaxJSONBytes+1024)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if buf.Len() > 64*1024 {
		t.Fatalf("fixture compressed to %d bytes; it should stay small to prove the guard is not just a size check on the input", buf.Len())
	}
	_, err := RenderTGS(context.Background(), buf.Bytes(), Options{})
	if err == nil {
		t.Fatal("RenderTGS(gzip bomb) = nil error")
	}
}

// The renderer runs untrusted animation data, so a deadline has to end a
// render rather than wait for it.
func TestRenderTGSHonoursDeadline(t *testing.T) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	if _, err := RenderTGS(ctx, tgs(t, movingSquare), Options{}); err == nil {
		t.Fatal("RenderTGS(expired context) = nil error")
	}
}

func TestRenderTGSIsConcurrencySafe(t *testing.T) {
	data := tgs(t, movingSquare)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			frames, err := RenderTGS(context.Background(), data, Options{Frames: 2})
			if err != nil {
				t.Errorf("concurrent RenderTGS: %v", err)
				return
			}
			if len(frames) != 2 {
				t.Errorf("concurrent RenderTGS = %d frames, want 2", len(frames))
			}
		})
	}
	wg.Wait()
}

func TestIsTGS(t *testing.T) {
	if !IsTGS([]byte{0x1f, 0x8b, 0x08}) {
		t.Fatal("gzip magic not recognised")
	}
	for _, data := range [][]byte{nil, {0x1f}, []byte("GIF89a"), {0x89, 'P', 'N', 'G'}} {
		if IsTGS(data) {
			t.Fatalf("IsTGS(%v) = true", data)
		}
	}
}

func TestFrameIndices(t *testing.T) {
	for _, tc := range []struct {
		total, count int
		want         []int
	}{
		{60, 5, []int{0, 15, 30, 44, 59}},
		{3, 5, []int{0, 1, 2}},
		{1, 5, []int{0}},
		{0, 5, nil},
		{60, 0, nil},
	} {
		if got := frameIndices(tc.total, tc.count); !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("frameIndices(%d, %d) = %v, want %v", tc.total, tc.count, got, tc.want)
		}
	}
}

// TestRealWorldLottie renders published Noto animated emoji. Hand-written
// fixtures use a layer or two; these use the shape groups, masks and
// interpolation actual stickers do, and they are what first showed ThorVG
// reaching host imports beyond heap growth.
func TestRealWorldLottie(t *testing.T) {
	for _, name := range []string{"bird", "pointing-up"} {
		t.Run(name, func(t *testing.T) {
			document, err := os.ReadFile(filepath.Join("testdata", name+".json")) //nolint:gosec // G304: fixed test fixture names.
			if err != nil {
				t.Fatal(err)
			}
			frames, err := Render(context.Background(), document, Options{})
			if err != nil {
				t.Fatalf("Render(%s): %v", name, err)
			}
			if len(frames) == 0 {
				t.Fatalf("Render(%s) produced no frames", name)
			}
			// A render that "succeeds" into a fully transparent canvas has
			// produced nothing, which is the shape a mishandled host import
			// takes: no error, no picture.
			for i, frame := range frames {
				opaque := 0
				for p := 3; p < len(frame.Pix); p += 4 {
					if frame.Pix[p] > 0 {
						opaque++
					}
				}
				if ratio := float64(opaque) / float64(len(frame.Pix)/4); ratio < 0.05 {
					t.Fatalf("frame %d of %s is %.1f%% opaque — the emoji did not draw", i, name, ratio*100)
				}
			}
		})
	}

	// ThorVG reaches a handful of stubbed imports on real content. This is the
	// set observed across 119 published Noto animated emoji, all of which
	// render correctly with those imports answered by zeros. A name outside it
	// is a code path that has never been checked, so it fails here rather than
	// silently producing whatever a zero happens to mean.
	known := map[string]bool{"b": true, "c": true, "fa": true, "j": true, "n": true}
	for name, calls := range ReachedHostImports() {
		if !known[name] {
			t.Fatalf("ThorVG reached host import %q (%d calls), which this package has never verified; "+
				"render the corpus in doc.go before widening the set", name, calls)
		}
	}
}
