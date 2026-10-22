package application

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"strings"
	"time"

	sdk "github.com/felinics/twilight/sdk"

	"github.com/felinics/memoh/internal/media/lottie"
)

// animationFrameCount is how many frames of an animated attachment reach the
// model. Five equidistant frames give a 1-3 second sticker roughly half a
// second of temporal resolution, which is enough to read an action, while
// keeping the per-sticker image cost bounded.
const animationFrameCount = 5

// animationRenderTimeout bounds a single animation render. Rasterising a
// sticker takes tens of milliseconds, so this is a ceiling on pathological
// input rather than a budget — and it is the deadline the sandbox aborts a
// runaway render on, which is the whole reason the renderer runs in one.
const animationRenderTimeout = 10 * time.Second

// visionBudget bounds how many images one turn hands the model.
//
// It is spent in two stages so a turn that carries more media than the budget
// still shows the model something of everything: an attachment offers all its
// frames, and once the budget is tight it contributes only its first. The
// zero value is a full budget.
type visionBudget struct{ used int }

// take returns the frames this attachment may contribute.
func (b *visionBudget) take(frames []sdk.ImagePart) []sdk.ImagePart {
	remaining := maxTurnVisionImages - b.used
	switch {
	case remaining <= 0:
		return nil
	case len(frames) <= remaining:
		b.used += len(frames)
		return frames
	default:
		// Not enough room for the expansion — keep the representative frame.
		b.used++
		return frames[:1]
	}
}

// inlineStoredImageParts turns a stored asset into direct vision input.
//
// An animated sticker becomes several frames while every other image stays a
// single part, so this is where the one-attachment-to-many-images fan-out
// begins and ends: the attachment itself is never duplicated, which keeps
// attachment paths, file fallbacks and size accounting counting it once.
func (s *Service) inlineStoredImageParts(ctx context.Context, botID, contentHash, declaredMime string) ([]sdk.ImagePart, error) {
	dataURLs, mime, err := s.inlineStoredImageDataURLs(ctx, botID, contentHash, declaredMime)
	if err != nil {
		return nil, err
	}
	parts := make([]sdk.ImagePart, 0, len(dataURLs))
	for _, dataURL := range dataURLs {
		parts = append(parts, sdk.ImagePart{Image: dataURL, MediaType: mime})
	}
	return parts, nil
}

// inlineStoredImageDataURLs returns one data URL per frame — one entry for an
// ordinary image, several for an animation — together with their shared MIME.
func (s *Service) inlineStoredImageDataURLs(ctx context.Context, botID, contentHash, declaredMime string) ([]string, string, error) {
	if s == nil || s.assetLoader == nil {
		return nil, "", errors.New("gateway asset loader not configured")
	}
	// Only animations are cached, so a hit already answers both "what is this"
	// and "what does it render to" — the asset never has to be opened.
	cacheKey := animationCacheKey(botID, contentHash)
	if cached, ok := s.animationFrames.get(cacheKey); ok {
		return cached, "image/png", nil
	}
	reader, assetMime, err := s.assetLoader.OpenForGateway(ctx, botID, contentHash)
	if err != nil {
		return nil, "", fmt.Errorf("open asset: %w", err)
	}
	defer func() { _ = reader.Close() }()

	// Peek before committing to a path: the stored MIME says whatever the
	// channel reported, and animated stickers routinely arrive labelled as
	// images.
	head := make([]byte, 2)
	n, err := io.ReadFull(reader, head)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return nil, "", fmt.Errorf("read asset: %w", err)
	}
	head = head[:n]
	replayed := io.MultiReader(bytes.NewReader(head), reader)

	if lottie.IsTGS(head) {
		dataURLs, err := renderAnimationDataURLs(ctx, replayed)
		if err != nil {
			return nil, "", err
		}
		s.animationFrames.put(cacheKey, dataURLs)
		return dataURLs, "image/png", nil
	}

	mime := strings.TrimSpace(declaredMime)
	if mime == "" {
		mime = strings.TrimSpace(assetMime)
	}
	dataURL, resolvedMime, err := encodeReaderAsDataURL(replayed, gatewayInlineAttachmentMaxBytes, "image", mime)
	if err != nil {
		return nil, "", err
	}
	return []string{dataURL}, resolvedMime, nil
}

// renderAnimationDataURLs rasterises an animated sticker into PNG frames the
// provider adapters accept.
func renderAnimationDataURLs(ctx context.Context, reader io.Reader) ([]string, error) {
	data, err := io.ReadAll(io.LimitReader(reader, gatewayInlineAttachmentMaxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read animated sticker: %w", err)
	}
	if int64(len(data)) > gatewayInlineAttachmentMaxBytes {
		return nil, errors.New("animated sticker exceeds the inline size limit")
	}
	ctx, cancel := context.WithTimeout(ctx, animationRenderTimeout)
	defer cancel()
	frames, err := lottie.RenderTGS(ctx, data, lottie.Options{Frames: animationFrameCount})
	if err != nil {
		return nil, fmt.Errorf("render animated sticker: %w", err)
	}
	// Equidistant sampling of an animation that barely moves yields repeats.
	// Identical frames cost the same as distinct ones and tell the model
	// nothing, so they collapse — a sticker that never moves becomes one image.
	dataURLs := make([]string, 0, len(frames))
	seen := make(map[string]bool, len(frames))
	for _, frame := range frames {
		encoded, err := encodeFrameAsPNGDataURL(frame)
		if err != nil {
			return nil, err
		}
		if seen[encoded] {
			continue
		}
		seen[encoded] = true
		dataURLs = append(dataURLs, encoded)
	}
	if len(dataURLs) == 0 {
		return nil, errors.New("animated sticker produced no frames")
	}
	return dataURLs, nil
}

// encodeFrameAsPNGDataURL flattens a frame onto white before encoding.
// Stickers are drawn for a chat background rather than a canvas of their own,
// and providers render an alpha channel inconsistently — an unflattened frame
// often arrives as a subject on black.
func encodeFrameAsPNGDataURL(frame image.Image) (string, error) {
	bounds := frame.Bounds()
	flattened := image.NewRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	draw.Draw(flattened, flattened.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	draw.Draw(flattened, flattened.Bounds(), frame, bounds.Min, draw.Over)
	var buf bytes.Buffer
	if err := png.Encode(&buf, flattened); err != nil {
		return "", fmt.Errorf("encode sticker frame: %w", err)
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}
