//go:build windows

package main

import (
	"context"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Fyne needs OpenGL 2.1. A VM's display adapter without 3D (VirtualBox,
// Hyper-V, a cloud VM) or a remote desktop session only offers Windows' own
// GDI Generic OpenGL 1.1: Fyne then fails to create the window and exits, so
// the tray icon disappears the moment the window is opened. The installer
// ships Mesa's software OpenGL (llvmpipe) in softgl\ next to the exe; a GUI
// process that finds no usable system OpenGL loads that one instead.

// glProbeArg runs probeOpenGL in a child process: once a process has loaded
// the system opengl32.dll it can't switch to another one.
const glProbeArg = "gl-probe"

const softGLDir = "softgl"

// useSoftwareGLIfNeeded points this process's opengl32.dll at softgl\ when
// the system's OpenGL can't run Fyne. Called before anything loads OpenGL.
func useSoftwareGLIfNeeded() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	dir := filepath.Join(filepath.Dir(exe), softGLDir)
	if _, err := os.Stat(filepath.Join(dir, "opengl32.dll")); err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, glProbeArg)
	cmd.SysProcAttr = &windows.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
	out, probeErr := cmd.Output()
	version := strings.TrimSpace(string(out))
	if probeErr == nil {
		log.Printf("[gl] system OpenGL %s", version)
		return
	}
	// llvmpipe, not Mesa's D3D12 driver: that one needs dxil.dll, not shipped.
	os.Setenv("GALLIUM_DRIVER", "llvmpipe")
	if err := windows.SetDllDirectory(dir); err != nil {
		log.Printf("[gl] system OpenGL unusable (%q), and %s can't be used: %v", version, dir, err)
		return
	}
	log.Printf("[gl] system OpenGL unusable (%q), using the software OpenGL in %s", version, dir)
}

// runGLProbe prints the system OpenGL version and exits 0 when it is 2.1 or
// newer, 1 otherwise.
func runGLProbe() int {
	version, err := probeOpenGL()
	if err != nil {
		os.Stdout.WriteString(err.Error())
		return 1
	}
	os.Stdout.WriteString(version)
	if !glVersionAtLeast(version, 2, 1) {
		return 1
	}
	return 0
}

type pixelFormatDescriptor struct {
	Size, Version                                               uint16
	Flags                                                       uint32
	PixelType, ColorBits, RedBits, RedShift, GreenBits          uint8
	GreenShift, BlueBits, BlueShift, AlphaBits, AlphaShift      uint8
	AccumBits, AccumRedBits, AccumGreenBits, AccumBlueBits      uint8
	AccumAlphaBits, DepthBits, StencilBits, AuxBuffers, LayerTy uint8
	Reserved                                                    uint8
	LayerMask, VisibleMask, DamageMask                          uint32
}

type wndClassEx struct {
	Size, Style                        uint32
	WndProc                            uintptr
	ClsExtra, WndExtra                 int32
	Instance, Icon, Cursor, Background uintptr
	MenuName, ClassName                *uint16
	IconSm                             uintptr
}

// probeOpenGL makes a context on a hidden window and returns GL_VERSION.
func probeOpenGL() (string, error) {
	// A WGL context belongs to the thread that made it current.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	user32 := windows.NewLazySystemDLL("user32.dll")
	gdi32 := windows.NewLazySystemDLL("gdi32.dll")
	opengl32 := windows.NewLazySystemDLL("opengl32.dll")
	// gdi32's SetPixelFormat only takes an OpenGL format once opengl32.dll is
	// loaded; before that it fails with ERROR_INVALID_PIXEL_FORMAT.
	if err := opengl32.Load(); err != nil {
		return "", err
	}

	// The window needs its own DC (CS_OWNDC) for a pixel format to stick:
	// on a system class like STATIC SetPixelFormat fails even with a real GPU.
	var instance windows.Handle
	if err := windows.GetModuleHandleEx(0, nil, &instance); err != nil {
		return "", err
	}
	const csOwnDC = 0x20
	className, _ := windows.UTF16PtrFromString("USBridgeGLProbe")
	wc := wndClassEx{Style: csOwnDC, WndProc: user32.NewProc("DefWindowProcW").Addr(), Instance: uintptr(instance), ClassName: className}
	wc.Size = uint32(unsafe.Sizeof(wc))
	if atom, _, err := user32.NewProc("RegisterClassExW").Call(uintptr(unsafe.Pointer(&wc))); atom == 0 {
		return "", err
	}
	const wsPopup = 0x80000000
	hwnd, _, err := user32.NewProc("CreateWindowExW").Call(0, uintptr(unsafe.Pointer(className)), 0, wsPopup, 0, 0, 1, 1, 0, 0, uintptr(instance), 0)
	if hwnd == 0 {
		return "", err
	}
	defer user32.NewProc("DestroyWindow").Call(hwnd)
	hdc, _, err := user32.NewProc("GetDC").Call(hwnd)
	if hdc == 0 {
		return "", err
	}
	defer user32.NewProc("ReleaseDC").Call(hwnd, hdc)

	const pfdDrawToWindow, pfdSupportOpenGL, pfdDoubleBuffer = 0x4, 0x20, 0x1
	pfd := pixelFormatDescriptor{Version: 1, Flags: pfdDrawToWindow | pfdSupportOpenGL | pfdDoubleBuffer, ColorBits: 32, DepthBits: 24}
	pfd.Size = uint16(unsafe.Sizeof(pfd))
	format, _, err := gdi32.NewProc("ChoosePixelFormat").Call(hdc, uintptr(unsafe.Pointer(&pfd)))
	if format == 0 {
		return "", err
	}
	if ok, _, err := gdi32.NewProc("SetPixelFormat").Call(hdc, format, uintptr(unsafe.Pointer(&pfd))); ok == 0 {
		return "", err
	}
	ctx, _, err := opengl32.NewProc("wglCreateContext").Call(hdc)
	if ctx == 0 {
		return "", err
	}
	defer opengl32.NewProc("wglDeleteContext").Call(ctx)
	if ok, _, err := opengl32.NewProc("wglMakeCurrent").Call(hdc, ctx); ok == 0 {
		return "", err
	}
	defer opengl32.NewProc("wglMakeCurrent").Call(0, 0)
	const glVersion = 0x1F02
	p, _, _ := opengl32.NewProc("glGetString").Call(glVersion)
	if p == 0 {
		return "", windows.ERROR_NOT_SUPPORTED
	}
	return windows.BytePtrToString(*(**byte)(unsafe.Pointer(&p))), nil
}
