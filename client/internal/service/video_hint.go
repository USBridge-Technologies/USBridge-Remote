package service

import (
	"image"
	"image/draw"
	"sync/atomic"
	"time"

	"golang.org/x/image/font"
)

// A short line of text shown over the video for a few seconds -- e.g. how
// to leave fullscreen. It goes through the Net Graph HUD's layer (the
// native Vulkan/Metal compositor where there is one, the CPU blit in
// ApplyNetGraphOverlay otherwise), because on Windows fullscreen is a
// standalone Vulkan window no Fyne toast can draw over. While the Net Graph
// itself is on, its HUD owns the layer and no hint is shown.
//
// Cost: the native layers get the canvas once (one 1 MB copy, then a quad
// per frame until cleared); the CPU path blends only the small box, and
// only while the hint is up. HideVideoHint (first key or click) takes it
// away early; when no hint is up it is one atomic load.

var (
	videoHintImg atomic.Pointer[image.RGBA] // CPU path: the box alone; nil = no hint
	videoHintGen atomic.Uint64
)

// ShowVideoHint shows text over the video for d, or until HideVideoHint.
func ShowVideoHint(text string, d time.Duration) {
	if text == "" || netGraphEnabled.Load() || netGraphFace == nil {
		return
	}
	canvas, box := buildVideoHint(text)
	gen := videoHintGen.Add(1)
	videoHintImg.Store(box)
	if push := netGraphMetalPush; push != nil {
		push(canvas)
	}
	time.AfterFunc(d, func() { hideVideoHint(gen) })
}

// HideVideoHint takes the hint away now (no-op when none is shown).
func HideVideoHint() {
	if videoHintImg.Load() == nil {
		return
	}
	hideVideoHint(videoHintGen.Load())
}

func hideVideoHint(gen uint64) {
	if videoHintGen.Load() != gen {
		return // a newer hint replaced this one
	}
	if videoHintImg.Swap(nil) == nil {
		return // already gone
	}
	if netGraphEnabled.Load() {
		return // the HUD took the layer over meanwhile
	}
	if clear := netGraphMetalClear; clear != nil {
		clear()
	}
}

// buildVideoHint returns the full HUD canvas for the native layers
// (vk_hud_set_pixels takes no other size; they anchor it bottom-right, so
// the box sits in its bottom-right corner on a transparent canvas) and the
// box alone for the CPU blit, which anchors bottom-right too.
func buildVideoHint(text string) (canvas, box *image.RGBA) {
	netGraphDrawMu.Lock()
	defer netGraphDrawMu.Unlock()
	const padX, padY = 14, 8
	tw := font.MeasureString(netGraphFace, text).Ceil()
	m := netGraphFace.Metrics()
	asc, desc := m.Ascent.Ceil(), m.Descent.Ceil()
	bw, bh := tw+2*padX, asc+desc+2*padY
	if bw > netGraphCanvasW {
		bw = netGraphCanvasW
	}

	box = image.NewRGBA(image.Rect(0, 0, bw, bh))
	bg := netGraphBg
	bg.A = 200
	draw.Draw(box, box.Bounds(), &image.Uniform{C: bg}, image.Point{}, draw.Src)
	netGraphDrawText(box, padX, padY+asc, text, netGraphGood)

	canvas = image.NewRGBA(image.Rect(0, 0, netGraphCanvasW, netGraphCanvasH))
	at := image.Rect(netGraphCanvasW-bw, netGraphCanvasH-bh, netGraphCanvasW, netGraphCanvasH)
	draw.Draw(canvas, at, box, image.Point{}, draw.Src)
	return canvas, box
}
