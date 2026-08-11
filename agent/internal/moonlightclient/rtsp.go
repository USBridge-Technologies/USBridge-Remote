package moonlightclient

import (
	"bufio"
	"bytes"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

// Sunshine's RTSP dialect is NOT real RTSP -- it's a simplified, closely
// related text protocol (request line + "Header: value\r\n" lines + blank
// line + optional body) spoken over a single short-lived TCP connection per
// message, one request/response pair at a time. This file follows
// moonlight-common-c/src/RtspConnection.c's performRtspHandshake exactly for
// the modern-Sunshine code path: useEnet=false (Sunshine gen7 uses TCP for
// RTSP, only the *control* channel uses ENet -- see control.go/enet.go),
// AppVersionQuad[0]>=7 so a single controlStreamId "streamid=control/13/0",
// rtspClientVersion=14, and APP_VERSION_AT_LEAST(7,1,431) so RTSP finishes
// with one combined PLAY ("/") instead of separate PLAY per stream.
//
// agent/internal/tailscale/stream_proxy.go's copyAndSnoop/scanServerPorts
// independently confirm the wire shape this parser expects (a "Transport"
// header containing "server_port=X" or "server_port=X-Y") -- see
// parseServerPort below.
const rtspClientVersion = 14

// controlStreamID matches RtspConnection.c: APP_VERSION_AT_LEAST(7,1,431)
// picks "streamid=control/13/0" (the "13" is useReliableUdp's value echoed
// back into the stream id, not a version number).
const controlStreamID = "streamid=control/13/0"

type rtspSession struct {
	target string // "rtsp://host:port" -- used as the request-URI for OPTIONS/DESCRIBE/SETUP

	sessionID string

	videoPort   int
	audioPort   int
	controlPort int

	// controlConnectData carries Sunshine's "X-SS-Connect-Data" SETUP
	// response header -- an opaque value that must be passed as the ENet
	// CONNECT command's "data" field (see enet.go/control.go) so Sunshine
	// can correlate the ENet control connection with this RTSP session.
	controlConnectData uint32

	// videoPingPayload/audioPingPayload carry Sunshine's "X-SS-Ping-Payload"
	// SETUP response headers (16 raw bytes each) -- see ping.go's doc
	// comment for why the caller must ping these UDP ports with this exact
	// payload before Sunshine will send any RTP. nil if Sunshine didn't
	// send the header (falls back to the legacy 4-byte "PING" payload).
	videoPingPayload []byte
	audioPingPayload []byte
}

// rtspMessage is the minimal parsed shape of one text-protocol response:
// status line + headers + optional body (DESCRIBE's SDP payload).
type rtspMessage struct {
	statusCode int
	headers    map[string]string
	body       string
}

func (m *rtspMessage) header(name string) string {
	// Sunshine's own RTSP responses use mixed case consistently, but be
	// case-insensitive on read since that's what the header name namespace
	// is defined to be.
	for k, v := range m.headers {
		if strings.EqualFold(k, name) {
			return v
		}
	}
	return ""
}

// dialRTSP opens the TCP connection to Sunshine's RTSP port. Each RTSP
// request in this implementation gets its own short-lived connection
// (mirrors transactRtspMessageTcp's connect-send-read-close pattern, since
// Sunshine's RTSP responses signal completion by closing the connection --
// there's no Content-Length-driven framing to allow connection reuse).
func dialRTSP(host string, port int, timeout time.Duration) (net.Conn, error) {
	return net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(port)), timeout)
}

// transact sends one RTSP request and parses the response. Connects fresh,
// sends the fully serialized request, then reads until the server closes
// the connection (matching moonlight-common-c's transactRtspMessageTcp,
// which reads in a loop until recv() returns 0).
func transact(host string, port int, method, uri string, extraHeaders map[string]string, body string, seq *int) (*rtspMessage, error) {
	conn, err := dialRTSP(host, port, 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("dial RTSP %s:%d: %w", host, port, err)
	}
	defer conn.Close()
	_ = conn.(*net.TCPConn).SetNoDelay(true)

	var buf bytes.Buffer
	fmt.Fprintf(&buf, "%s %s RTSP/1.0\r\n", method, uri)
	fmt.Fprintf(&buf, "CSeq: %d\r\n", *seq)
	*seq++
	fmt.Fprintf(&buf, "X-GS-ClientVersion: %d\r\n", rtspClientVersion)
	for k, v := range extraHeaders {
		fmt.Fprintf(&buf, "%s: %s\r\n", k, v)
	}
	if body != "" {
		fmt.Fprintf(&buf, "Content-length: %d\r\n", len(body))
	}
	buf.WriteString("\r\n")
	buf.WriteString(body)

	if err := conn.SetDeadline(time.Now().Add(15 * time.Second)); err != nil {
		return nil, err
	}
	if _, err := conn.Write(buf.Bytes()); err != nil {
		return nil, fmt.Errorf("send %s: %w", method, err)
	}

	raw, err := readAllUntilClose(conn)
	if err != nil {
		return nil, fmt.Errorf("read %s response: %w", method, err)
	}

	return parseRTSPResponse(raw)
}

func readAllUntilClose(conn net.Conn) ([]byte, error) {
	var out bytes.Buffer
	buf := make([]byte, 16384)
	for {
		n, err := conn.Read(buf)
		if n > 0 {
			out.Write(buf[:n])
		}
		if err != nil {
			// EOF (clean close) is the expected end-of-response signal, not
			// an error, for this text protocol.
			break
		}
	}
	return out.Bytes(), nil
}

func parseRTSPResponse(raw []byte) (*rtspMessage, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("empty response (connection closed with no data)")
	}
	r := bufio.NewReader(bytes.NewReader(raw))
	statusLine, err := r.ReadString('\n')
	if err != nil {
		return nil, fmt.Errorf("read status line: %w", err)
	}
	// "RTSP/1.0 200 OK\r\n"
	fields := strings.SplitN(strings.TrimSpace(statusLine), " ", 3)
	if len(fields) < 2 {
		return nil, fmt.Errorf("malformed status line: %q", statusLine)
	}
	code, err := strconv.Atoi(fields[1])
	if err != nil {
		return nil, fmt.Errorf("malformed status code in %q: %w", statusLine, err)
	}

	msg := &rtspMessage{statusCode: code, headers: map[string]string{}}
	for {
		line, err := r.ReadString('\n')
		trimmed := strings.TrimRight(line, "\r\n")
		if trimmed == "" {
			break
		}
		if idx := strings.Index(trimmed, ":"); idx > 0 {
			name := strings.TrimSpace(trimmed[:idx])
			val := strings.TrimSpace(trimmed[idx+1:])
			msg.headers[name] = val
		}
		if err != nil {
			break
		}
	}

	rest, _ := r.Peek(r.Buffered())
	remaining := raw[len(raw)-r.Buffered():]
	_ = rest
	msg.body = string(remaining)

	return msg, nil
}

// parseServerPort extracts the port from a Transport header value like
// "unicast;server_port=48000-48001;source=..." -- same field Sunshine's
// SETUP response carries that agent/internal/tailscale/stream_proxy.go's
// scanServerPorts independently snoops for its own (unrelated) relay
// purposes. Falls back to fallback if parsing fails, matching
// RtspConnection.c's parseServerPortFromTransport behavior.
func parseServerPort(transport string, fallback int) int {
	idx := strings.Index(transport, "server_port=")
	if idx < 0 {
		return fallback
	}
	rest := transport[idx+len("server_port="):]
	end := strings.IndexAny(rest, "-;, \r\n")
	if end < 0 {
		end = len(rest)
	}
	port, err := strconv.Atoi(rest[:end])
	if err != nil || port <= 0 || port > 65535 {
		return fallback
	}
	return port
}

// pingPayloadBytes returns the raw bytes of Sunshine's X-SS-Ping-Payload
// SETUP response header, or nil if absent/wrong length. RtspConnection.c
// treats this as a raw 16-byte binary blob copied directly out of the
// header value (via strlen + memcpy, no hex/base64 decoding) -- Sunshine
// only ever generates payloads made of header-safe bytes, so treating the
// parsed header string's raw bytes as the payload (rather than decoding
// it as hex, which would be wrong) matches the reference exactly.
func pingPayloadBytes(header string) []byte {
	if len(header) != 16 {
		return nil
	}
	return []byte(header)
}

// performRTSPHandshake runs OPTIONS, DESCRIBE, three SETUPs
// (audio/video/control), ANNOUNCE, then a single PLAY -- the exact sequence
// and payloads of moonlight-common-c's performRtspHandshake for a modern
// Sunshine host. rtspHostPort is host:port as returned by /launch's
// sessionUrl0 (with the rtsp:// scheme already stripped by nvhttp.go).
func performRTSPHandshake(rtspHostPort string, cfg Config) (*rtspSession, error) {
	host, portStr, err := net.SplitHostPort(rtspHostPort)
	if err != nil {
		return nil, fmt.Errorf("parse RTSP session URL %q: %w", rtspHostPort, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return nil, fmt.Errorf("parse RTSP port %q: %w", portStr, err)
	}

	s := &rtspSession{target: fmt.Sprintf("rtsp://%s:%d", host, port)}
	seq := 1

	// OPTIONS
	resp, err := transact(host, port, "OPTIONS", s.target, nil, "", &seq)
	if err != nil {
		return nil, fmt.Errorf("RTSP OPTIONS: %w", err)
	}
	if resp.statusCode != 200 {
		return nil, fmt.Errorf("RTSP OPTIONS: status %d", resp.statusCode)
	}

	// DESCRIBE -- response SDP tells us the negotiated codec, but this
	// package always requests H.264 (see buildSDP) and doesn't need to
	// parse the description beyond confirming success.
	resp, err = transact(host, port, "DESCRIBE", s.target, map[string]string{
		"Accept":            "application/sdp",
		"If-Modified-Since": "Thu, 01 Jan 1970 00:00:00 GMT",
	}, "", &seq)
	if err != nil {
		return nil, fmt.Errorf("RTSP DESCRIBE: %w", err)
	}
	if resp.statusCode != 200 {
		return nil, fmt.Errorf("RTSP DESCRIBE: status %d", resp.statusCode)
	}

	// SETUP streamid=audio/0/0
	resp, err = transact(host, port, "SETUP", "streamid=audio/0/0", map[string]string{
		"Transport":         "unicast;X-GS-ClientPort=50000-50001",
		"If-Modified-Since": "Thu, 01 Jan 1970 00:00:00 GMT",
	}, "", &seq)
	if err != nil {
		return nil, fmt.Errorf("RTSP SETUP audio: %w", err)
	}
	if resp.statusCode != 200 {
		return nil, fmt.Errorf("RTSP SETUP audio: status %d", resp.statusCode)
	}
	s.audioPort = parseServerPort(resp.header("Transport"), cfg.baseUDPPort()+2) // +11 offset fallback
	s.audioPingPayload = pingPayloadBytes(resp.header("X-SS-Ping-Payload"))
	sid := resp.header("Session")
	if sid == "" {
		return nil, fmt.Errorf("RTSP SETUP audio: missing Session header")
	}
	s.sessionID = strings.SplitN(sid, ";", 2)[0]

	// SETUP streamid=video/0/0
	resp, err = transact(host, port, "SETUP", "streamid=video/0/0", map[string]string{
		"Session":           s.sessionID,
		"Transport":         "unicast;X-GS-ClientPort=50000-50001",
		"If-Modified-Since": "Thu, 01 Jan 1970 00:00:00 GMT",
	}, "", &seq)
	if err != nil {
		return nil, fmt.Errorf("RTSP SETUP video: %w", err)
	}
	if resp.statusCode != 200 {
		return nil, fmt.Errorf("RTSP SETUP video: status %d", resp.statusCode)
	}
	s.videoPort = parseServerPort(resp.header("Transport"), cfg.baseUDPPort())
	s.videoPingPayload = pingPayloadBytes(resp.header("X-SS-Ping-Payload"))

	// SETUP streamid=control/13/0
	resp, err = transact(host, port, "SETUP", controlStreamID, map[string]string{
		"Session":           s.sessionID,
		"Transport":         "unicast;X-GS-ClientPort=50000-50001",
		"If-Modified-Since": "Thu, 01 Jan 1970 00:00:00 GMT",
	}, "", &seq)
	if err != nil {
		return nil, fmt.Errorf("RTSP SETUP control: %w", err)
	}
	if resp.statusCode != 200 {
		return nil, fmt.Errorf("RTSP SETUP control: status %d", resp.statusCode)
	}
	s.controlPort = parseServerPort(resp.header("Transport"), cfg.baseUDPPort()+1)
	if cd := resp.header("X-SS-Connect-Data"); cd != "" {
		if v, err := strconv.ParseUint(cd, 10, 32); err == nil {
			s.controlConnectData = uint32(v)
		}
	}

	// ANNOUNCE — carries the SDP describing our desired stream config.
	sdp := buildSDP(rtspClientVersion, "127.0.0.1", s.videoPort, cfg.Width, cfg.Height, cfg.FPS, cfg.BitrateKbps)
	resp, err = transact(host, port, "ANNOUNCE", controlStreamID, map[string]string{
		"Session":      s.sessionID,
		"Content-type": "application/sdp",
	}, sdp, &seq)
	if err != nil {
		return nil, fmt.Errorf("RTSP ANNOUNCE: %w", err)
	}
	if resp.statusCode != 200 {
		return nil, fmt.Errorf("RTSP ANNOUNCE: status %d", resp.statusCode)
	}

	// PLAY "/" — modern Sunshine (APP_VERSION_AT_LEAST(7,1,431)) starts
	// everything with one combined PLAY instead of per-stream PLAY.
	resp, err = transact(host, port, "PLAY", "/", map[string]string{
		"Session": s.sessionID,
	}, "", &seq)
	if err != nil {
		return nil, fmt.Errorf("RTSP PLAY: %w", err)
	}
	if resp.statusCode != 200 {
		return nil, fmt.Errorf("RTSP PLAY: status %d", resp.statusCode)
	}

	return s, nil
}
