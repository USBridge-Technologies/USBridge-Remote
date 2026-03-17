//go:build windows
// +build windows

package service

import (
	"image"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"usbridge-client/internal/models"
)

// TestWindowsLiveStreamDecoding — интеграционный тест: запуск видео как в интерфейсе,
// слушаем статический порт DefaultVideoUDPPort (55000), проверяем что декодирование даёт кадры.
// Требует: устройство уже стримит на этот ПК на порт 55000 (запустите видео в приложении на устройстве).
func TestWindowsLiveStreamDecoding(t *testing.T) {
	cfg := models.DefaultConfig()
	cfg.VideoUDPPort = models.DefaultVideoUDPPort
	gs := NewGStreamerService(cfg)

	var framesReceived int64
	gs.SetOnFrameReceived(func(img image.Image) {
		atomic.AddInt64(&framesReceived, 1)
	})

	if err := gs.ConnectToRTP(); err != nil {
		if strings.Contains(err.Error(), "Failed to change state to PLAYING") {
			t.Skipf("порт %d занят или недоступен — закройте приложение и другие процессы, затем повторите: %v", models.DefaultVideoUDPPort, err)
		}
		t.Fatalf("ConnectToRTP (как в UI): %v", err)
	}
	defer gs.Disconnect()

	// Ждём кадры с живого потока (как на экране управления)
	const waitTimeout = 15 * time.Second
	const minFrames = 1
	deadline := time.Now().Add(waitTimeout)
	for time.Now().Before(deadline) {
		n := atomic.LoadInt64(&framesReceived)
		if n >= minFrames {
			t.Logf("OK: живой поток на порту %d — получено %d кадров, декодирование работает", models.DefaultVideoUDPPort, n)
			return
		}
		time.Sleep(200 * time.Millisecond)
	}

	n := atomic.LoadInt64(&framesReceived)
	if n == 0 {
		t.Skipf("нет кадров за %v: поток на порту %d не идёт — запустите видео на устройстве и повторите тест", waitTimeout, models.DefaultVideoUDPPort)
	}
	t.Logf("OK: получено %d кадров с порта %d", n, models.DefaultVideoUDPPort)
}
