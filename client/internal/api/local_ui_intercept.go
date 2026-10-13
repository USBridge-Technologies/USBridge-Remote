package api

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	_ "image/png" // registers the PNG format with image.DecodeConfig
	"sync"
	"time"

	"usbridge-client/internal/localui"
)

// Local ui.parse interception: when enabled (see SetLocalUIParser), the MCP
// proxy answers the ui.parse tool call itself instead of forwarding it to
// the device -- running the same three-model pipeline (YOLOv8 icon
// detector + DBNet text detector + SVTR text recognizer) locally via ONNX
// Runtime on this machine's CPU or GPU (CoreML/DirectML/OpenVINO, see
// internal/localui/onnx.go's acceleratorEP) instead of the device's RK3566
// NPU. It still fetches the raw screenshot from the device (screen.get_image
// is cheap -- no OCR/YOLO runs on the device for that call, see that tool's
// description), only the expensive detection/recognition work moves local.
//
// This exists because ui.parse at 1920x1080 tiles DBNet into 6 native-
// resolution passes on the device's single-core NPU (~20s end to end, see
// mcpProxyTimeout's doc comment); the same models on this machine's CPU or
// an accelerator EP finish in well under 5s (see internal/localui's package
// doc comment and its benchmarked numbers).
//
// Every other MCP tool call is untouched -- an MCP client sees identical
// behavior and JSON shape regardless of which backend answered, aside from
// the added informational "_backend" field localui.Result carries. The one
// exception is tools/list itself (see injectLocalUIParseTool below): a
// hardware KVM backend already advertises ui.parse on its own, but a
// software Agent backend (agent/internal/api/mcp.go) has no detector at all
// and so never lists it -- without injection here, an MCP client that
// discovers tools via tools/list rather than calling ui.parse blind would
// never learn this local offload exists when talking to an Agent, even
// though tryLocalUIParse above answers it perfectly well either way.

// minimal local mirrors of the device's MCP JSON-RPC envelope -- just
// enough fields to parse a tools/call request and re-serialize a
// tools/call response; the client repo doesn't share a Go module with the
// device repo, so these aren't imported.
type mcpEnvelope struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	// omitempty: this struct doubles as the shape for both requests (which
	// have a method) and responses (which don't) -- without it, every
	// response built from this struct (tryLocalUIParse's own reply below,
	// and injectLocalUIParseTool's re-marshaled tools/list) would gain a
	// spurious "method":"" field no real MCP response carries.
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  json.RawMessage `json:"error,omitempty"`
}

type mcpToolCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

type mcpContent struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	Data     string `json:"data,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
}

type mcpToolResult struct {
	Content []mcpContent `json:"content"`
	IsError bool         `json:"isError,omitempty"`
}

// localUIState holds the optional local ui.parse offload, guarded by a
// mutex since Start/SetLocalUIParser can race with in-flight requests.
type localUIState struct {
	mu      sync.RWMutex
	parser  *localui.Parser
	enabled bool
}

var globalLocalUI localUIState

// SetLocalUIParser installs (or, with p==nil, removes) the local ui.parse
// backend. Called once at startup if the user's settings enable it (see
// models.AppConfig's LocalUIParse* fields) -- building the Parser is
// relatively expensive (loads 3 ONNX models), so it happens once, not
// per-request.
func SetLocalUIParser(p *localui.Parser) {
	globalLocalUI.mu.Lock()
	defer globalLocalUI.mu.Unlock()
	if globalLocalUI.parser != nil && globalLocalUI.parser != p {
		globalLocalUI.parser.Close()
	}
	globalLocalUI.parser = p
	globalLocalUI.enabled = p != nil
}

// LocalUIParserActive reports whether local ui.parse offload is currently
// wired up (for GUI status display).
func LocalUIParserActive() bool {
	globalLocalUI.mu.RLock()
	defer globalLocalUI.mu.RUnlock()
	return globalLocalUI.enabled
}

// GetLocalUIParser returns the currently installed local ui.parse backend,
// or nil if none is active (see SetLocalUIParser). Exposed so other
// features can reuse the already-loaded ONNX models instead of duplicating
// the "optional accelerator, load once at startup" lifecycle handled here
// -- e.g. the AI Vision live video overlay (internal/service/ai_vision.go),
// which runs the exact same detector against live frames instead of a
// screenshot fetched on demand.
func GetLocalUIParser() *localui.Parser {
	globalLocalUI.mu.RLock()
	defer globalLocalUI.mu.RUnlock()
	return globalLocalUI.parser
}

// tryLocalScreenImage intercepts a tools/call "screen.get_image" request
// and answers it directly from the video decode pipeline's already-
// rendered frame (see live_frame.go) when a session is actively
// streaming -- no ONNX/localui involved, no gating on the local ui.parse
// offload setting, just "is a frame already sitting here decoded". The
// device (Agent or hardware KVM) is never asked at all in that case.
//
// Exists because the Agent's own screen.get_image capture can be
// unreliable or outright broken depending on the target's desktop
// session (confirmed live: a KDE/Wayland Agent either hangs on an
// unanswerable portal consent dialog, or -- after routing around that --
// returns a blank black frame, since KWin blocks plain X11 capture over
// XWayland as a privacy measure) -- none of which matters when the
// Client is already displaying the real picture right now. Falls through
// (handled=false) exactly like tryLocalUIParse when no live frame shows
// up within the timeout (no stream open), so the normal forward-to-device
// path runs unchanged for a headless/no-video MCP session.
func tryLocalScreenImage(reqBody []byte) (respBody []byte, handled bool) {
	var env mcpEnvelope
	if err := json.Unmarshal(reqBody, &env); err != nil || env.Method != "tools/call" {
		return nil, false
	}
	var call mcpToolCallParams
	if err := json.Unmarshal(env.Params, &call); err != nil || call.Name != "screen.get_image" {
		return nil, false
	}

	png, ok := RequestLiveFrame(liveFrameWaitTimeout)
	if !ok {
		return nil, false
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(png))
	if err != nil {
		return nil, false // malformed frame -- let the device answer instead of erroring the call
	}

	toolResult := mcpToolResult{Content: []mcpContent{
		{Type: "image", MimeType: "image/png", Data: base64.StdEncoding.EncodeToString(png)},
		{Type: "text", Text: fmt.Sprintf(`{"width":%d,"height":%d}`, cfg.Width, cfg.Height)},
	}}
	resultJSON, err := json.Marshal(toolResult)
	if err != nil {
		return nil, false
	}
	out := mcpEnvelope{JSONRPC: "2.0", ID: env.ID, Result: resultJSON}
	body, err := json.Marshal(out)
	if err != nil {
		return nil, false
	}
	return body, true
}

// tryLocalUIParse intercepts a tools/call request for "ui.parse" and
// answers it locally if enabled. Returns (responseBody, true, nil) on a
// successful local answer, (nil, false, nil) if this request isn't a
// ui.parse call or local offload isn't enabled (caller should forward to
// the device as usual), or (nil, true, err) if it WAS a ui.parse call
// meant to be answered locally but local processing failed (caller should
// report the error rather than silently falling back, so a broken local
// setup doesn't disguise itself as an empty ui.parse result).
func tryLocalUIParse(client *USBClient, reqBody []byte) (respBody []byte, handled bool, err error) {
	globalLocalUI.mu.RLock()
	parser := globalLocalUI.parser
	enabled := globalLocalUI.enabled
	globalLocalUI.mu.RUnlock()
	if !enabled || parser == nil {
		return nil, false, nil
	}

	var env mcpEnvelope
	if err := json.Unmarshal(reqBody, &env); err != nil {
		return nil, false, nil // not our concern, let the normal path report the parse error
	}
	if env.Method != "tools/call" {
		return nil, false, nil
	}
	var call mcpToolCallParams
	if err := json.Unmarshal(env.Params, &call); err != nil {
		return nil, false, nil
	}
	if call.Name != "ui.parse" {
		return nil, false, nil
	}
	var args struct {
		Text bool `json:"text"`
	}
	// Best-effort: absent/malformed arguments just means the default
	// (fast, no text) -- ui.parse takes no required arguments today, so a
	// caller that predates this flag sends "{}" or omits arguments
	// entirely, neither of which should be treated as an error.
	_ = json.Unmarshal(call.Arguments, &args)

	// Preferred: the live video frame as raw pixels -- no PNG encode of it
	// here and no PNG decode of it inside the parser.
	var markedPNG []byte
	var result *localui.Result
	if frame, ok := RequestLiveRGBA(liveFrameWaitTimeout); ok {
		if args.Text {
			markedPNG, result, err = parser.ParseRGBA(frame)
		} else {
			markedPNG, result, err = parser.ParseIconsOnlyMarkedRGBA(frame)
		}
		if err != nil {
			return nil, true, fmt.Errorf("local ui.parse: %w", err)
		}
		return localParseResponse(env, markedPNG, result)
	}

	imgBytes, err := screenImageForLocalParse(client)
	if err != nil {
		return nil, true, fmt.Errorf("local ui.parse: fetch screen.get_image from device: %w", err)
	}

	if args.Text {
		markedPNG, result, err = parser.Parse(imgBytes)
	} else {
		// Fast path (default): icon_detect alone, no dbnet/svtr OCR --
		// measured live at ~300-400ms on a 3840x2160 frame vs. several
		// seconds for full Parse (see ParseIconsOnlyMarked's doc comment).
		// A caller that only needs to click something should never pay
		// OCR's cost; one that also wants to read text passes
		// arguments:{"text":true} and gets the slower, complete pass.
		markedPNG, result, err = parser.ParseIconsOnlyMarked(imgBytes)
	}
	if err != nil {
		return nil, true, fmt.Errorf("local ui.parse: %w", err)
	}
	return localParseResponse(env, markedPNG, result)
}

// localParseResponse builds the ui.parse tools/call reply (annotated image +
// JSON result) shared by the raw-pixel and PNG input paths.
func localParseResponse(env mcpEnvelope, markedPNG []byte, result *localui.Result) ([]byte, bool, error) {
	payload, err := json.Marshal(result)
	if err != nil {
		return nil, true, fmt.Errorf("local ui.parse: marshal result: %w", err)
	}

	toolResult := mcpToolResult{Content: []mcpContent{
		{Type: "image", MimeType: "image/png", Data: base64.StdEncoding.EncodeToString(markedPNG)},
		{Type: "text", MimeType: "application/json", Text: string(payload)},
	}}
	resultJSON, err := json.Marshal(toolResult)
	if err != nil {
		return nil, true, fmt.Errorf("local ui.parse: marshal tool result: %w", err)
	}

	out := mcpEnvelope{JSONRPC: "2.0", ID: env.ID, Result: resultJSON}
	body, err := json.Marshal(out)
	if err != nil {
		return nil, true, fmt.Errorf("local ui.parse: marshal response: %w", err)
	}
	return body, true, nil
}

// liveFrameWaitTimeout bounds how long screenImageForLocalParse waits for
// the video decode path to hand over a frame (see live_frame.go) before
// giving up and falling back to a device round-trip. Must cover not just a
// frame's arrival (8-16ms at 60-120fps) but maybeServeLiveFrame's own PNG
// encode of it, which the requester blocks on too -- confirmed live at
// 3840x2160 that a bare image/png.Encode of a single frame alone already
// costs 110-120ms on a highly-compressible synthetic frame, before even
// counting a real desktop's far less compressible pixels or the queueing
// delay until the next decoded frame after the request arrives. 300ms
// measured live as too tight: maybeServeLiveFrame would start encoding but
// RequestLiveFrame's own timeout (and its deferred liveFrameWanted reset)
// fired first, so SubmitLiveFrame's CompareAndSwap always lost the race
// and every call fell through to fetchScreenImage regardless of an active
// session. 2s comfortably covers a real encode with margin; still far
// short of the 30s a caller would otherwise wait on fetchScreenImage's own
// device round-trip when no video session is open at all.
const liveFrameWaitTimeout = 2 * time.Second

// screenImageForLocalParse gets the screenshot local ui.parse decodes,
// preferring a frame the video decode path is already producing (no extra
// network round-trip or device-side capture) over fetchScreenImage's full
// device round-trip. Only ever skips the device entirely when a video
// session is actively streaming AND the operator is looking at it in the
// GUI right now -- with no video session open (the common case for a
// headless/background MCP agent), RequestLiveFrame just times out after
// liveFrameWaitTimeout and this falls back to fetchScreenImage exactly as
// before this existed.
func screenImageForLocalParse(client *USBClient) ([]byte, error) {
	if png, ok := RequestLiveFrame(liveFrameWaitTimeout); ok {
		return png, nil
	}
	return fetchScreenImage(client)
}

// fetchScreenImage calls the device's screen.get_image MCP tool and
// extracts the raw PNG bytes -- this is the one call the local path still
// makes to the device (cheap: no OCR/YOLO runs there for it).
func fetchScreenImage(client *USBClient) ([]byte, error) {
	req := mcpEnvelope{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`"local-ui-parse"`),
		Method:  "tools/call",
	}
	params := mcpToolCallParams{Name: "screen.get_image"}
	paramsJSON, _ := json.Marshal(params)
	req.Params = paramsJSON
	reqBody, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	respBody, err := client.PostRawWithTimeout("/api/mcp", reqBody, mcpProxyTimeout)
	if err != nil {
		return nil, err
	}
	var resp mcpEnvelope
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	if len(resp.Error) > 0 {
		return nil, fmt.Errorf("device error: %s", string(resp.Error))
	}
	var result mcpToolResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("decode tool result: %w", err)
	}
	for _, c := range result.Content {
		if c.Type == "image" && c.Data != "" {
			return base64.StdEncoding.DecodeString(c.Data)
		}
	}
	return nil, fmt.Errorf("screen.get_image returned no image content")
}

// localUIParseToolDef is the ui.parse MCP tool definition injected into a
// tools/list response by injectLocalUIParseTool below -- deliberately kept
// close to the hardware KVM's own ui.parse description (see
// usbridge_service/web/handlers.go's mcpTools) so an MCP client follows the
// exact same "compute the box center, pass it plus image_width/image_height
// to mouse.action's move_to/click_at/double_click_at" recipe regardless of
// which backend it's talking to -- that recipe now works against a software
// Agent backend too (see agent/internal/api/mcp_mouse_absolute.go).
func localUIParseToolDef() map[string]any {
	return map[string]any{
		"name":        "ui.parse",
		"description": "Detect and read graphical UI elements on the current screen via this CLIENT's local ONNX pipeline (YOLOv8 icon/element detector + DBNet+SVTR text detector/recognizer, running on this machine's CPU/GPU -- see internal/localui) instead of the connected device's own hardware: fetches a screenshot from the device (cheap) and runs detection here. By default (no arguments, or text:false) this ONLY runs icon/element detection -- a fraction of a second -- and returns just the clickable boxes, no text: enough to locate and click_at something. Pass {\"text\":true} to also run OCR (DBNet+SVTR) and get recognized text back, which costs several more seconds -- use it only when you actually need to read what's on screen, not before every click. Returns an annotated PNG (red boxes = clickable icons/elements, green boxes = recognized text, only present with text:true) alongside a JSON list of every box with its pixel bbox (and, for text, the recognized string) plus image_width/image_height for that capture. Each entry also carries a best-effort label (nearby/overlapping text, text:true only) so you can search for an element by name. To act on a result: compute the box center ((x1+x2)/2, (y1+y2)/2) and pass it plus image_width/image_height to mouse.action's move_to/click_at/double_click_at -- the same recipe works whether the connected device is a hardware KVM or a software Agent. Call it when you need to LOCATE something you don't already have coordinates for, not routinely after every action.",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"text": map[string]any{
					"type":        "boolean",
					"description": "Also run OCR and return recognized text (several seconds slower). Default false: icons/elements only, sub-second.",
				},
			},
			"additionalProperties": false,
		},
	}
}

// mcpToolsListResult mirrors the shape of a tools/list JSON-RPC result --
// just enough to read and re-append to the "tools" array without needing
// the full MCPTool type from either backend repo (this client doesn't
// import either).
type mcpToolsListResult struct {
	Tools []json.RawMessage `json:"tools"`
}

// injectLocalUIParseTool adds ui.parse to a tools/list response when local
// offload is enabled (see SetLocalUIParser) and the backend's own answer
// doesn't already advertise it -- a hardware KVM backend already includes
// its own real ui.parse (left untouched, never duplicated), so this only
// ever actually adds anything when reqBody/respBody are talking to a
// software Agent. Best-effort: any parse failure of either body just
// returns respBody unchanged, so a malformed or unexpected response is
// still reported to the caller as-is rather than hidden behind an
// injection bug here.
func injectLocalUIParseTool(reqBody, respBody []byte) []byte {
	globalLocalUI.mu.RLock()
	enabled := globalLocalUI.enabled
	globalLocalUI.mu.RUnlock()
	if !enabled {
		return respBody
	}

	var reqEnv mcpEnvelope
	if err := json.Unmarshal(reqBody, &reqEnv); err != nil || reqEnv.Method != "tools/list" {
		return respBody
	}

	var respEnv mcpEnvelope
	if err := json.Unmarshal(respBody, &respEnv); err != nil || len(respEnv.Result) == 0 {
		return respBody
	}
	var result mcpToolsListResult
	if err := json.Unmarshal(respEnv.Result, &result); err != nil {
		return respBody
	}

	for _, raw := range result.Tools {
		var t struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(raw, &t) == nil && t.Name == "ui.parse" {
			return respBody // already advertised by the backend itself
		}
	}

	toolJSON, err := json.Marshal(localUIParseToolDef())
	if err != nil {
		return respBody
	}
	result.Tools = append(result.Tools, toolJSON)
	resultJSON, err := json.Marshal(result)
	if err != nil {
		return respBody
	}
	respEnv.Result = resultJSON
	out, err := json.Marshal(respEnv)
	if err != nil {
		return respBody
	}
	return out
}
