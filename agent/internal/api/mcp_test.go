package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"usbridge_agent/internal/clipboard"
)

// mcpTestAbsoluteCall records one AbsoluteEvent invocation so tests can
// assert click_at/double_click_at's press+release sequencing and the
// pixel->0..32767 conversion, without caring about the OS-specific
// controller each platform actually drives.
type mcpTestAbsoluteCall struct {
	mask  uint8
	x, y  uint16
	wheel int8
}

// mcpTestInput records every call so tests can assert dispatch, and lets a
// specific action be made to fail to exercise the isError:true path.
type mcpTestInput struct {
	failAction    string
	lastKey       uint8
	lastCombo     [2]uint8
	lastText      string
	lastMouse     string
	absoluteCalls []mcpTestAbsoluteCall
}

func (i *mcpTestInput) Key(k uint8) error {
	i.lastKey = k
	if i.failAction == "key" {
		return errTestInput
	}
	return nil
}
func (i *mcpTestInput) Combo(mod, k uint8) error {
	i.lastCombo = [2]uint8{mod, k}
	return nil
}
func (i *mcpTestInput) Text(t string) error {
	i.lastText = t
	return nil
}
func (i *mcpTestInput) MouseMove(int8, int8) error { i.lastMouse = "move"; return nil }
func (i *mcpTestInput) MouseClick(uint8) error     { i.lastMouse = "click"; return nil }
func (i *mcpTestInput) MouseScroll(int8) error     { i.lastMouse = "scroll"; return nil }
func (i *mcpTestInput) MouseAction(uint8, int8, int8, int8) error {
	i.lastMouse = "action"
	return nil
}
func (i *mcpTestInput) AbsoluteEvent(mask uint8, x, y uint16, wheel int8) error {
	i.lastMouse = "absolute"
	i.absoluteCalls = append(i.absoluteCalls, mcpTestAbsoluteCall{mask: mask, x: x, y: y, wheel: wheel})
	if i.failAction == "absolute" {
		return errTestInput
	}
	return nil
}

var errTestInput = &testError{"simulated input failure"}

type testError struct{ msg string }

func (e *testError) Error() string { return e.msg }

type mcpTestScreen struct{}

func (mcpTestScreen) Snapshot() (*ScreenSnapshot, error) {
	return &ScreenSnapshot{Format: "png-base64", Width: 100, Height: 50, ImageBase64: "Zm9v"}, nil
}

// solidPNG encodes a w*h image filled with c -- used to build real,
// decodable before/after screenshots for click_at's screen-diff tests
// (mcpTestScreen's own "Zm9v" placeholder isn't valid PNG, which is fine
// for tests that don't care about the diff, but click_at's diff tests need
// bytes screenChangePercent can actually decode).
func solidPNG(t *testing.T, w, h int, c color.Color) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode test PNG: %v", err)
	}
	return buf.Bytes()
}

// mcpTestSeqScreen returns a different image on each successive Snapshot
// call (clamped to the last one once exhausted) so click_at tests can
// control exactly what the "before" and "after" diff captures see.
type mcpTestSeqScreen struct {
	width, height int
	images        [][]byte // raw PNG bytes, returned base64-encoded
	calls         int
}

func (s *mcpTestSeqScreen) Snapshot() (*ScreenSnapshot, error) {
	idx := s.calls
	if idx >= len(s.images) {
		idx = len(s.images) - 1
	}
	s.calls++
	return &ScreenSnapshot{
		Format:      "png-base64",
		Width:       s.width,
		Height:      s.height,
		ImageBase64: base64.StdEncoding.EncodeToString(s.images[idx]),
	}, nil
}

// mcpTestApp implements Application with just enough behavior to exercise
// the MCP handler in isolation, independent of clipboard_test.go's stubApp
// (whose Screen() intentionally returns a nil snapshot, which would panic
// mcpScreenGetImage's field access).
type mcpTestApp struct {
	input  *mcpTestInput
	screen interface {
		Snapshot() (*ScreenSnapshot, error)
	}
}

func (a *mcpTestApp) Status() SystemStatus { return SystemStatus{} }
func (a *mcpTestApp) DeviceInfo() DeviceInfoResponse {
	return DeviceInfoResponse{AgentOS: "darwin", Count: 0}
}
func (a *mcpTestApp) ReplaceDevices([]DeviceRequest) error { return nil }
func (a *mcpTestApp) ClearDevices() error                  { return nil }
func (a *mcpTestApp) Input() interface {
	Key(uint8) error
	Combo(uint8, uint8) error
	Text(string) error
	MouseMove(int8, int8) error
	MouseClick(uint8) error
	MouseScroll(int8) error
	MouseAction(uint8, int8, int8, int8) error
	AbsoluteEvent(uint8, uint16, uint16, int8) error
} {
	return a.input
}
func (a *mcpTestApp) Screen() interface {
	Snapshot() (*ScreenSnapshot, error)
} {
	if a.screen != nil {
		return a.screen
	}
	return mcpTestScreen{}
}
func (a *mcpTestApp) VideoDevices() []VideoDeviceInfo              { return nil }
func (a *mcpTestApp) VirtualDisplaySupported() bool                { return false }
func (a *mcpTestApp) VideoFec() (int, bool, bool, bool)            { return 0, false, false, false }
func (a *mcpTestApp) SetVideoFec(int, bool) (bool, error)          { return false, nil }
func (a *mcpTestApp) VideoSource() (VideoSourceInfo, bool)         { return VideoSourceInfo{}, false }
func (a *mcpTestApp) SetVideoSource(VideoSourceInfo) (bool, error) { return false, nil }
func (a *mcpTestApp) RawHIDSupported() bool                        { return false }
func (a *mcpTestApp) SunshineOutputName() string                   { return "" }
func (a *mcpTestApp) SetSunshineOutputName(string) error           { return nil }
func (a *mcpTestApp) SunshineStreamHost() string                   { return "" }
func (a *mcpTestApp) SunshineAdminPort() int                       { return 0 }
func (a *mcpTestApp) SubmitMoonlightPIN(string) error              { return nil }
func (a *mcpTestApp) CurrentVideoCodec() string                    { return "" }
func (a *mcpTestApp) SupportedVideoCodecs() []string               { return []string{"h264"} }
func (a *mcpTestApp) Color444Status() (bool, bool)                 { return false, false }
func (a *mcpTestApp) HdrStatus() (bool, bool)                      { return false, false }
func (a *mcpTestApp) PyroWaveColorStatus() (bool, bool)            { return false, false }
func (a *mcpTestApp) AudioSinks() ([]AudioSink, error)             { return nil, nil }
func (a *mcpTestApp) CurrentAudioSink() (string, error)            { return "", nil }
func (a *mcpTestApp) SetAudioSink(string) error                    { return nil }
func (a *mcpTestApp) TailscaleStatus() *TailscaleStatusInfo        { return nil }
func (a *mcpTestApp) RegisterTailscale(context.Context, string, string) (*TailscaleStatusInfo, error) {
	return nil, nil
}
func (a *mcpTestApp) Clipboard() *clipboard.Manager { return nil }
func (a *mcpTestApp) ClipboardMaxBytes() int64      { return 0 }

const mcpTestSecret = "test-mcp-master-key"

func mcpTestServer() (*Server, *mcpTestInput) {
	input := &mcpTestInput{}
	srv := NewServerWithAuth(&mcpTestApp{input: input}, []byte(mcpTestSecret), 0)
	return srv, input
}

// mcpTestServerWithScreen is mcpTestServer but with a caller-supplied Screen
// backend, for click_at/move_to tests that need real decodable PNGs (a
// before/after diff) rather than mcpTestScreen's fixed "Zm9v" placeholder.
func mcpTestServerWithScreen(screen interface {
	Snapshot() (*ScreenSnapshot, error)
}) (*Server, *mcpTestInput) {
	input := &mcpTestInput{}
	srv := NewServerWithAuth(&mcpTestApp{input: input, screen: screen}, []byte(mcpTestSecret), 0)
	return srv, input
}

func mcpTestRequest(t *testing.T, srv *Server, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sig := CalculateHMAC(http.MethodPost, "/api/mcp", ts, string(raw), []byte(mcpTestSecret))

	req := httptest.NewRequest(http.MethodPost, "/api/mcp", bytes.NewReader(raw))
	req.Header.Set("X-Auth-Signature", sig)
	req.Header.Set("X-Auth-Timestamp", ts)
	req.RemoteAddr = "127.0.0.1:54321"

	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	return rec
}

func decodeRPC(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response: %v (body: %s)", err, rec.Body.String())
	}
	return out
}

func TestMCP_ToolsList_ExcludesHardwareOnlyTools(t *testing.T) {
	srv, _ := mcpTestServer()
	rec := mcpTestRequest(t, srv, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/list"})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", rec.Code, rec.Body.String())
	}
	out := decodeRPC(t, rec)
	result := out["result"].(map[string]any)
	tools := result["tools"].([]any)

	names := map[string]bool{}
	for _, raw := range tools {
		tool := raw.(map[string]any)
		names[tool["name"].(string)] = true
	}
	for _, want := range []string{"screen.get_image", "keyboard.send", "mouse.action", "device.info"} {
		if !names[want] {
			t.Errorf("tools/list missing %q", want)
		}
	}
	for _, mustNotHave := range []string{"mountdrive.start", "scripts.run", "pcpanel.button", "media.insert"} {
		if names[mustNotHave] {
			t.Errorf("tools/list should not advertise hardware-only tool %q", mustNotHave)
		}
	}
}

func TestMCP_ToolsCall_KeyboardSend(t *testing.T) {
	srv, input := mcpTestServer()
	rec := mcpTestRequest(t, srv, map[string]any{
		"jsonrpc": "2.0", "id": 2, "method": "tools/call",
		"params": map[string]any{"name": "keyboard.send", "arguments": map[string]any{"action": "text", "text": "hello"}},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", rec.Code, rec.Body.String())
	}
	if input.lastText != "hello" {
		t.Fatalf("Input().Text not called with expected text, got %q", input.lastText)
	}
	out := decodeRPC(t, rec)
	result := out["result"].(map[string]any)
	if isErr, _ := result["isError"].(bool); isErr {
		t.Fatalf("unexpected isError:true, body: %s", rec.Body.String())
	}
}

func TestMCP_ToolsCall_KeyboardSend_MissingField(t *testing.T) {
	srv, _ := mcpTestServer()
	rec := mcpTestRequest(t, srv, map[string]any{
		"jsonrpc": "2.0", "id": 3, "method": "tools/call",
		"params": map[string]any{"name": "keyboard.send", "arguments": map[string]any{"action": "key"}},
	})
	out := decodeRPC(t, rec)
	result := out["result"].(map[string]any)
	if isErr, _ := result["isError"].(bool); !isErr {
		t.Fatalf("expected isError:true for missing key_code, body: %s", rec.Body.String())
	}
}

func TestMCP_ToolsCall_InputFailureSurfacesAsToolError(t *testing.T) {
	input := &mcpTestInput{failAction: "key"}
	srv := NewServerWithAuth(&mcpTestApp{input: input}, []byte(mcpTestSecret), 0)
	kc := 40
	rec := mcpTestRequest(t, srv, map[string]any{
		"jsonrpc": "2.0", "id": 4, "method": "tools/call",
		"params": map[string]any{"name": "keyboard.send", "arguments": map[string]any{"action": "key", "key_code": kc}},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("OS-level tool failure must still be HTTP 200 (JSON-RPC result, not transport error): got %d", rec.Code)
	}
	out := decodeRPC(t, rec)
	result := out["result"].(map[string]any)
	if isErr, _ := result["isError"].(bool); !isErr {
		t.Fatalf("expected isError:true, body: %s", rec.Body.String())
	}
}

func TestMCP_ToolsCall_ScreenGetImage(t *testing.T) {
	srv, _ := mcpTestServer()
	rec := mcpTestRequest(t, srv, map[string]any{
		"jsonrpc": "2.0", "id": 5, "method": "tools/call",
		"params": map[string]any{"name": "screen.get_image", "arguments": map[string]any{}},
	})
	out := decodeRPC(t, rec)
	result := out["result"].(map[string]any)
	content := result["content"].([]any)
	if len(content) != 2 {
		t.Fatalf("expected 2 content blocks (image + dims), got %d: %v", len(content), content)
	}
	imgBlock := content[0].(map[string]any)
	if imgBlock["type"] != "image" || imgBlock["data"] != "Zm9v" || imgBlock["mimeType"] != "image/png" {
		t.Fatalf("unexpected image block: %v", imgBlock)
	}
}

func TestMCP_ResourcesRead_Instructions(t *testing.T) {
	srv, _ := mcpTestServer()
	rec := mcpTestRequest(t, srv, map[string]any{
		"jsonrpc": "2.0", "id": 6, "method": "resources/read",
		"params": map[string]any{"uri": "usbridge://instructions"},
	})
	out := decodeRPC(t, rec)
	result := out["result"].(map[string]any)
	contents := result["contents"].([]any)
	first := contents[0].(map[string]any)
	if first["mimeType"] != "text/markdown" {
		t.Fatalf("unexpected mimeType: %v", first["mimeType"])
	}
}

func TestMCP_UnknownMethod(t *testing.T) {
	srv, _ := mcpTestServer()
	rec := mcpTestRequest(t, srv, map[string]any{"jsonrpc": "2.0", "id": 7, "method": "bogus/method"})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404, body: %s", rec.Code, rec.Body.String())
	}
}

func TestMCP_RejectsUnsigned(t *testing.T) {
	srv, _ := mcpTestServer()
	raw, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/list"})
	req := httptest.NewRequest(http.MethodPost, "/api/mcp", bytes.NewReader(raw))
	req.RemoteAddr = "127.0.0.1:54321"
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatalf("expected an unsigned request to be rejected, got 200: %s", rec.Body.String())
	}
}

// ─── move_to/click_at/double_click_at ──────────────────────────────────────

// mcpCallTool is a small helper around mcpTestRequest+decodeRPC for
// tools/call tests below: returns the decoded "result" object.
func mcpCallTool(t *testing.T, srv *Server, name string, args map[string]any) map[string]any {
	t.Helper()
	rec := mcpTestRequest(t, srv, map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{"name": name, "arguments": args},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", rec.Code, rec.Body.String())
	}
	out := decodeRPC(t, rec)
	result, ok := out["result"].(map[string]any)
	if !ok {
		t.Fatalf("no result object in response: %s", rec.Body.String())
	}
	return result
}

// mcpFirstText returns the "text" field of a tool result's first content
// block, failing the test if there isn't one.
func mcpFirstText(t *testing.T, result map[string]any) string {
	t.Helper()
	content, ok := result["content"].([]any)
	if !ok || len(content) == 0 {
		t.Fatalf("no content blocks in result: %v", result)
	}
	block, ok := content[0].(map[string]any)
	if !ok {
		t.Fatalf("content[0] is not an object: %v", content[0])
	}
	text, _ := block["text"].(string)
	return text
}

func TestMCP_ToolsList_AdvertisesAbsoluteMouseActions(t *testing.T) {
	srv, _ := mcpTestServer()
	rec := mcpTestRequest(t, srv, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/list"})
	out := decodeRPC(t, rec)
	tools := out["result"].(map[string]any)["tools"].([]any)

	var mouseTool map[string]any
	for _, raw := range tools {
		tool := raw.(map[string]any)
		if tool["name"] == "mouse.action" {
			mouseTool = tool
			break
		}
	}
	if mouseTool == nil {
		t.Fatal("tools/list missing mouse.action")
	}
	schema := mouseTool["inputSchema"].(map[string]any)
	props := schema["properties"].(map[string]any)
	enumRaw := props["action"].(map[string]any)["enum"].([]any)
	enum := map[string]bool{}
	for _, v := range enumRaw {
		enum[v.(string)] = true
	}
	for _, want := range []string{"move_to", "click_at", "double_click_at"} {
		if !enum[want] {
			t.Errorf("mouse.action's action enum missing %q: %v", want, enumRaw)
		}
	}
	for _, want := range []string{"x", "y", "screen_width", "screen_height", "capture_after_ms"} {
		if _, ok := props[want]; !ok {
			t.Errorf("mouse.action inputSchema missing property %q", want)
		}
	}
}

func TestMCP_MouseAction_ClickAt_RequiresCoordinates(t *testing.T) {
	srv, _ := mcpTestServer()
	result := mcpCallTool(t, srv, "mouse.action", map[string]any{"action": "click_at", "x": 10})
	if isErr, _ := result["isError"].(bool); !isErr {
		t.Fatalf("expected isError:true for missing y/screen_width/screen_height, got %v", result)
	}
}

func TestMCP_MouseAction_ClickAt_InvalidButton(t *testing.T) {
	srv, _ := mcpTestServer()
	result := mcpCallTool(t, srv, "mouse.action", map[string]any{
		"action": "click_at", "x": 10, "y": 10, "screen_width": 100, "screen_height": 100, "button": 9,
	})
	if isErr, _ := result["isError"].(bool); !isErr {
		t.Fatalf("expected isError:true for button=9, got %v", result)
	}
}

func TestMCP_MouseAction_ClickAt_ConvertsPixelsAndSequencesPressRelease(t *testing.T) {
	srv, input := mcpTestServer()
	wantX, wantY := pixelToAbsoluteXY(100, 50, 200, 100)

	result := mcpCallTool(t, srv, "mouse.action", map[string]any{
		"action": "click_at", "x": 100, "y": 50, "screen_width": 200, "screen_height": 100,
	})
	if isErr, _ := result["isError"].(bool); isErr {
		t.Fatalf("unexpected isError:true: %v", result)
	}

	if len(input.absoluteCalls) != 2 {
		t.Fatalf("expected 2 AbsoluteEvent calls (press+release), got %d: %+v", len(input.absoluteCalls), input.absoluteCalls)
	}
	press, release := input.absoluteCalls[0], input.absoluteCalls[1]
	if press.mask != 0x01 {
		t.Errorf("press mask = %#x, want 0x01 (left button, default)", press.mask)
	}
	if release.mask != 0 {
		t.Errorf("release mask = %#x, want 0 (button up)", release.mask)
	}
	for _, call := range []mcpTestAbsoluteCall{press, release} {
		if call.x != wantX || call.y != wantY {
			t.Errorf("call coords = (%d,%d), want (%d,%d) from pixelToAbsoluteXY(100,50,200,100)", call.x, call.y, wantX, wantY)
		}
	}
}

func TestMCP_MouseAction_ClickAt_DefaultButtonIsLeft(t *testing.T) {
	srv, input := mcpTestServer()
	mcpCallTool(t, srv, "mouse.action", map[string]any{
		"action": "click_at", "x": 5, "y": 5, "screen_width": 10, "screen_height": 10,
	})
	if len(input.absoluteCalls) == 0 || input.absoluteCalls[0].mask != 0x01 {
		t.Fatalf("expected default button to be left (mask 0x01), got %+v", input.absoluteCalls)
	}
}

func TestMCP_MouseAction_ClickAt_RightButton(t *testing.T) {
	srv, input := mcpTestServer()
	mcpCallTool(t, srv, "mouse.action", map[string]any{
		"action": "click_at", "x": 5, "y": 5, "screen_width": 10, "screen_height": 10, "button": 2,
	})
	if len(input.absoluteCalls) == 0 || input.absoluteCalls[0].mask != 0x02 {
		t.Fatalf("expected right button (mask 0x02), got %+v", input.absoluteCalls)
	}
}

func TestMCP_MouseAction_DoubleClickAt_FourAbsoluteEvents(t *testing.T) {
	srv, input := mcpTestServer()
	result := mcpCallTool(t, srv, "mouse.action", map[string]any{
		"action": "double_click_at", "x": 5, "y": 5, "screen_width": 10, "screen_height": 10,
	})
	if isErr, _ := result["isError"].(bool); isErr {
		t.Fatalf("unexpected isError:true: %v", result)
	}
	if len(input.absoluteCalls) != 4 {
		t.Fatalf("expected 4 AbsoluteEvent calls (press,release,press,release), got %d: %+v", len(input.absoluteCalls), input.absoluteCalls)
	}
	wantMasks := []uint8{0x01, 0, 0x01, 0}
	for i, want := range wantMasks {
		if input.absoluteCalls[i].mask != want {
			t.Errorf("call[%d].mask = %#x, want %#x", i, input.absoluteCalls[i].mask, want)
		}
	}
}

func TestMCP_MouseAction_MoveTo_SingleZeroMaskEvent(t *testing.T) {
	srv, input := mcpTestServer()
	result := mcpCallTool(t, srv, "mouse.action", map[string]any{
		"action": "move_to", "x": 5, "y": 5, "screen_width": 10, "screen_height": 10,
	})
	if isErr, _ := result["isError"].(bool); isErr {
		t.Fatalf("unexpected isError:true: %v", result)
	}
	if len(input.absoluteCalls) != 1 {
		t.Fatalf("expected exactly 1 AbsoluteEvent call for move_to, got %d: %+v", len(input.absoluteCalls), input.absoluteCalls)
	}
	if input.absoluteCalls[0].mask != 0 {
		t.Errorf("move_to must not press a button, mask = %#x", input.absoluteCalls[0].mask)
	}
	text := mcpFirstText(t, result)
	if text != "ok" {
		t.Errorf(`move_to without capture_after_ms should return plain "ok", got %q`, text)
	}
}

func TestMCP_MouseAction_ClickAt_ScreenChangeDetected(t *testing.T) {
	black := solidPNG(t, 20, 20, color.RGBA{0, 0, 0, 255})
	white := solidPNG(t, 20, 20, color.RGBA{255, 255, 255, 255})
	screen := &mcpTestSeqScreen{width: 20, height: 20, images: [][]byte{black, white}}
	srv, _ := mcpTestServerWithScreen(screen)

	result := mcpCallTool(t, srv, "mouse.action", map[string]any{
		"action": "click_at", "x": 5, "y": 5, "screen_width": 20, "screen_height": 20,
	})
	var payload map[string]any
	if err := json.Unmarshal([]byte(mcpFirstText(t, result)), &payload); err != nil {
		t.Fatalf("decode click_at result payload: %v (text: %s)", err, mcpFirstText(t, result))
	}
	pct, _ := payload["screen_changed_pct"].(float64)
	if pct < 90 {
		t.Errorf("screen_changed_pct = %v, want ~100 for a full black->white change", pct)
	}
	if visibly, _ := payload["screen_visibly_changed"].(bool); !visibly {
		t.Errorf("screen_visibly_changed = %v, want true", visibly)
	}
}

func TestMCP_MouseAction_ClickAt_NoScreenChangeDetected(t *testing.T) {
	gray := solidPNG(t, 20, 20, color.RGBA{128, 128, 128, 255})
	screen := &mcpTestSeqScreen{width: 20, height: 20, images: [][]byte{gray, gray}}
	srv, _ := mcpTestServerWithScreen(screen)

	result := mcpCallTool(t, srv, "mouse.action", map[string]any{
		"action": "click_at", "x": 5, "y": 5, "screen_width": 20, "screen_height": 20,
	})
	var payload map[string]any
	if err := json.Unmarshal([]byte(mcpFirstText(t, result)), &payload); err != nil {
		t.Fatalf("decode click_at result payload: %v", err)
	}
	if visibly, _ := payload["screen_visibly_changed"].(bool); visibly {
		t.Errorf("screen_visibly_changed = true for an identical before/after screenshot, want false")
	}
}

func TestMCP_MouseAction_ClickAt_ConfirmFalseSkipsDiffAndStillClicks(t *testing.T) {
	black := solidPNG(t, 20, 20, color.RGBA{0, 0, 0, 255})
	white := solidPNG(t, 20, 20, color.RGBA{255, 255, 255, 255})
	screen := &mcpTestSeqScreen{width: 20, height: 20, images: [][]byte{black, white}}
	srv, input := mcpTestServerWithScreen(screen)

	result := mcpCallTool(t, srv, "mouse.action", map[string]any{
		"action": "click_at", "x": 5, "y": 5, "screen_width": 20, "screen_height": 20, "confirm": false,
	})
	var payload map[string]any
	if err := json.Unmarshal([]byte(mcpFirstText(t, result)), &payload); err != nil {
		t.Fatalf("decode click_at result payload: %v", err)
	}
	if _, has := payload["screen_changed_pct"]; has {
		t.Errorf("confirm:false still computed a diff: %v", payload)
	}
	if payload["status"] != "ok" {
		t.Errorf("status = %v, want ok", payload["status"])
	}
	if got := len(input.absoluteCalls); got != 2 {
		t.Errorf("injected %d absolute events, want 2 (press+release)", got)
	}
}

func TestMCP_MouseAction_ClickAt_UndecodableScreenshotOmitsDiffButStillClicks(t *testing.T) {
	// Default mcpTestScreen returns "Zm9v" ("foo"), not a valid PNG -- the
	// diff must fail silently (best-effort) rather than block the click.
	srv, input := mcpTestServer()
	result := mcpCallTool(t, srv, "mouse.action", map[string]any{
		"action": "click_at", "x": 5, "y": 5, "screen_width": 10, "screen_height": 10,
	})
	if isErr, _ := result["isError"].(bool); isErr {
		t.Fatalf("undecodable before/after screenshot must not fail the click itself: %v", result)
	}
	if len(input.absoluteCalls) != 2 {
		t.Fatalf("click must still have happened despite the diff failing, got %d AbsoluteEvent calls", len(input.absoluteCalls))
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(mcpFirstText(t, result)), &payload); err != nil {
		t.Fatalf("decode click_at result payload: %v", err)
	}
	if _, present := payload["screen_changed_pct"]; present {
		t.Errorf("screen_changed_pct should be omitted when the diff couldn't be computed, got %v", payload)
	}
}

func TestMCP_MouseAction_ClickAt_InputFailureSurfacesAsToolError(t *testing.T) {
	input := &mcpTestInput{failAction: "absolute"}
	srv := NewServerWithAuth(&mcpTestApp{input: input}, []byte(mcpTestSecret), 0)
	result := mcpCallTool(t, srv, "mouse.action", map[string]any{
		"action": "click_at", "x": 5, "y": 5, "screen_width": 10, "screen_height": 10,
	})
	if isErr, _ := result["isError"].(bool); !isErr {
		t.Fatalf("expected isError:true when AbsoluteEvent fails, got %v", result)
	}
}

// TestMCP_MouseAction_LegacyAbsoluteActionNowWorks guards against the bug
// this change also fixed: the "absolute" action was declared in this same
// tool's schema but applyMouse's switch had no case for it, so it silently
// did nothing (fell to default: return nil) instead of erroring OR moving
// the mouse. See server.go's applyMouse.
func TestMCP_MouseAction_LegacyAbsoluteActionNowWorks(t *testing.T) {
	srv, input := mcpTestServer()
	result := mcpCallTool(t, srv, "mouse.action", map[string]any{
		"action": "absolute", "x": 16000, "y": 8000, "button_state": 1,
	})
	if isErr, _ := result["isError"].(bool); isErr {
		t.Fatalf("unexpected isError:true: %v", result)
	}
	if len(input.absoluteCalls) != 1 {
		t.Fatalf("expected action=\"absolute\" to reach AbsoluteEvent exactly once, got %d calls", len(input.absoluteCalls))
	}
	call := input.absoluteCalls[0]
	if call.x != 16000 || call.y != 8000 || call.mask != 1 {
		t.Fatalf("AbsoluteEvent called with %+v, want x=16000 y=8000 mask=1", call)
	}
}

func (s *mcpTestApp) VirtualDisplayPrimary() bool                 { return false }
func (s *mcpTestApp) SetVirtualDisplayPrimary(bool) (bool, error) { return false, nil }
func (s *mcpTestApp) SetVirtualDisplayExclusive(bool) error       { return nil }
func (s *mcpTestApp) ClientSessionEnded()                         {}
func (s *mcpTestApp) ClientSessionStarting(time.Duration)         {}
