package controller

import (
	"math"

	"usbridge-client/internal/models"
	"usbridge-client/internal/service"
)

// SelectBestResolution picks the highest resolution the client's hardware
// decoder supports, preferring the host's native resolution:
//  1. Resolve the host's native resolution from (nativeW, nativeH), falling
//     back to the largest mode in captureModes when native is unreported (0).
//  2. If the client's hardware decoder (maxW, maxH) supports the native
//     resolution, return native untouched.
//  3. Otherwise pick the largest mode in captureModes that fits within
//     (maxW, maxH) (preferring the same aspect ratio), or proportionally scale
//     native down to fit (maxW, maxH) rounded to even pixel dimensions.
func SelectBestResolution(nativeW, nativeH int, captureModes []models.VideoCaptureMode, maxW, maxH int) (int, int) {
	if nativeW <= 0 || nativeH <= 0 {
		for _, m := range captureModes {
			if m.Width*m.Height > nativeW*nativeH {
				nativeW, nativeH = m.Width, m.Height
			}
		}
	}
	if nativeW <= 0 || nativeH <= 0 {
		return 0, 0
	}
	if maxW <= 0 || maxH <= 0 {
		return nativeW, nativeH
	}
	if nativeW <= maxW && nativeH <= maxH {
		return nativeW, nativeH
	}

	// Native exceeds hardware decoder ceiling: look for the highest fitting
	// capture mode, preferring ones matching native's aspect ratio.
	nativeAspect := float64(nativeW) / float64(nativeH)
	bestW, bestH := 0, 0
	bestAspectMatch := false
	for _, m := range captureModes {
		if m.Width <= 0 || m.Height <= 0 || m.Width > maxW || m.Height > maxH {
			continue
		}
		aspect := float64(m.Width) / float64(m.Height)
		sameAspect := math.Abs(aspect-nativeAspect) < 0.03
		area := m.Width * m.Height
		bestArea := bestW * bestH
		if (sameAspect && !bestAspectMatch) || (sameAspect == bestAspectMatch && area > bestArea) {
			bestW, bestH = m.Width, m.Height
			bestAspectMatch = sameAspect
		}
	}
	if bestW > 0 && bestH > 0 {
		return bestW, bestH
	}

	// No capture mode fit: scale native proportionally to fit (maxW, maxH)
	// with even dimensions for video encoders/decoders.
	scaleW := float64(maxW) / float64(nativeW)
	scaleH := float64(maxH) / float64(nativeH)
	scale := math.Min(scaleW, scaleH)
	w := int(math.Floor(float64(nativeW)*scale)) &^ 1
	h := int(math.Floor(float64(nativeH)*scale)) &^ 1
	if w < 2 {
		w = 2
	}
	if h < 2 {
		h = 2
	}
	return w, h
}

func selectResolutionForInfo(info *models.VideoInfoData, videoMode string) (int, int) {
	if info == nil {
		return 0, 0
	}
	maxW, maxH := service.MaxHardwareDecodeResolution(videoMode)
	return SelectBestResolution(info.Width, info.Height, info.CaptureModes, maxW, maxH)
}
