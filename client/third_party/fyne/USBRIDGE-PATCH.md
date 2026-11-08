# Fyne v2.8.2 with one USBridge patch

Upstream fyne.io/fyne/v2 v2.8.2, without cmd/, testdata and upstream tests.
The changes are in `internal/driver/mobile/driver.go`, plus the test
`internal/driver/mobile/driver_startup_test.go`, and the Windows one below.

## The bug

On Android and iOS, `fyne.Do` called on any goroutine before `Run()` ran its
function right there on that goroutine: the queue did not exist yet, and
`IsMainGoroutine` is true for everyone until the main goroutine is set. So
background work laid out widgets while main was still building the window.
Fyne's theme stack and text shaper are not safe for that, and the client
crashed on launch with `index out of range` in `HarfbuzzShaper.Shape` or a nil
theme in `theme.Padding()`.

## The patch

`NewGoMobileDriver` creates the queue and marks the main goroutine, and before
`Run()` only main runs functions directly. Everything else waits in the queue
for the event loop, as the desktop driver does.

## Windows: a window with no viewport

`setDarkMode` in `internal/driver/glfw/window_windows.go` now returns when the
window has no viewport. Since Fyne 2.8 every settings change calls it on all
windows, and on Windows it dereferenced the viewport. The client's keep-alive
window is never shown, so it has none: any settings change closed the client
with a nil pointer panic.

## Upgrading Fyne

Copy the new version here, reapply the patch and rerun the test:
`go test -tags mobile -race ./internal/driver/mobile/ -run TestDoBeforeRun`
from this directory. Drop this copy if upstream fixes it.
