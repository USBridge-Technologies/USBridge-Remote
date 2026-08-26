# Agent TODO

## Live X11/Wayland session-type re-detection on Linux

`capture.AutoCaptureMode()` (internal/capture/env_linux.go) only picks the
Sunshine capture backend ("x11" vs "kms") once — at agent startup, or
whenever Sunshine is explicitly restarted via `SetSunshineCaptureMode`. It
never re-evaluates while the process keeps running, because
`capture.GetLinuxEnv()` reads `DISPLAY`/`WAYLAND_DISPLAY` from the agent
process's own environment, which is fixed for the lifetime of that process.

This is a no-op in the common case: if the agent starts headless (systemd,
before login), it has neither var set, correctly lands on "kms", and that
stays correct through a login into a Wayland session (AutoCaptureMode's
Wayland answer is also "kms" — see its doc comment).

The one real gap: **greeter/headless start → user logs into an X11 desktop
session**. The already-running headless agent instance has no way to notice
the live session is now X11 and stays on "kms" (the broken combo on
NVIDIA — see the fix in agent/internal/capture/env_linux.go and
agent/internal/ui/window.go's matching comment) until something restarts
the whole agent process *from within that X11 session* — reopening the GUI
window over an already-running headless instance does not do this, since
that only attaches a UI to the existing process; the process's own
environment is unchanged.

Potential fix, modeled on the Windows agent's `app.NotifySessionChange`
(triggered by the Windows service's WTS_SESSION_LOGON/WTS_CONSOLE_CONNECT
handler, see cmd/usbridge_agent/service_windows.go): watch systemd-logind
for a session becoming active/its Type changing (e.g. subscribe to
`org.freedesktop.login1` session PropertiesChanged over D-Bus, or poll
`loginctl show-session -p Type`), and when it flips to "x11"/"wayland",
recompute `capture.AutoCaptureMode()` against that session's actual
`DISPLAY`/`WAYLAND_DISPLAY` (not this process's own, stale environment —
would need to read them from the session's own environment, e.g. via
`/proc/<session-leader-pid>/environ` or the same `/run/usbridge-agent/...`
mechanism `autostart_linux.go`'s SDDM Xsetup hook already uses for a related
problem) and restart Sunshine if it changed.

Not implemented yet — flagged during a debugging session but the user
declined to pursue it immediately (bigger, riskier change than a one-line
fix: touches session-state IPC, not just local detection).
