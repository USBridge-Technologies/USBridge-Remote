#!/usr/bin/env node
// USBridge MCP bridge -- lets an MCP client that only speaks stdio (Claude
// Desktop, and most others) reach the tool server running inside a
// USBridge web-client browser tab, which structurally can never accept an
// inbound connection itself (no raw TCP/HTTP listen in a browser sandbox --
// see the Scripts & AI tab's own instructions for why this exists at all
// instead of the plain HTTP proxy the desktop app uses).
//
// Topology: Claude Desktop spawns THIS process over stdio (the transport
// every MCP client already supports natively, no "url"/SSE support
// required on Claude's end). This process in turn runs a plain local
// WebSocket *server* -- Node has no sandbox restriction on listening -- and
// the browser tab (open on the Scripts & AI page) connects OUT to it as a
// client, the one direction a browser CAN always initiate. Once that
// connection exists, this process is a dumb bidirectional relay: it has no
// idea what MCP even is, it just shuttles newline-delimited JSON-RPC text
// between stdin/stdout and the one connected WebSocket. All real protocol
// logic (initialize, tools/list, tools/call) is handled entirely on the
// browser side by the official @modelcontextprotocol/sdk running there
// (see web/ai_vision.js's sibling MCP server module) -- keeping this file
// to one real dependency (ws, for the WebSocket server itself -- reusing
// the standard, widely-used package rather than hand-rolling WebSocket
// framing) instead of also pulling in the MCP SDK on the Node side for
// logic it would just be forwarding anyway.
//
// Buffering matters here specifically because of a race no amount of
// retrying on Claude's end fixes: Claude Desktop sends its `initialize`
// JSON-RPC request the instant this process starts, which is typically
// BEFORE the user has the Scripts & AI browser tab open yet (Claude
// Desktop starts configured MCP servers at its own launch, independent of
// whether/when the user opens the site). Every line arriving on stdin
// before a browser client is connected is queued, then flushed in order
// the moment one connects -- see pendingFromClaude below. The same applies
// in reverse in practice too (a slow tab reload momentarily drops the
// socket): messages simply queue again until the next connection.
import { WebSocketServer } from "ws";
import { createInterface } from "node:readline";

function parseArgs(argv) {
  const args = { port: 9001, token: "" };
  for (let i = 0; i < argv.length; i++) {
    if (argv[i] === "--port") args.port = Number(argv[++i]);
    else if (argv[i] === "--token") args.token = argv[++i];
  }
  return args;
}

const { port, token } = parseArgs(process.argv.slice(2));
if (!token) {
  // Anyone/anything else running locally could otherwise open a
  // WebSocket to 127.0.0.1:<port> and answer Claude's tool calls in the
  // browser tab's place -- a local-privilege spoofing risk, not just a
  // theoretical one, since the whole point of this process is that it
  // accepts unauthenticated-by-origin local connections (unlike
  // mcp_proxy.go's HTTP proxy, which explicitly rejects anything carrying
  // a browser Origin header for exactly this reason). The token (shown
  // alongside the download link/config block on the Scripts & AI page,
  // see scripts_tab_wasm's own instructions) is the only thing that
  // closes that gap here.
  process.stderr.write("usbridge mcp-bridge: --token is required\n");
  process.exit(1);
}

let activeSocket = null;
const pendingFromClaude = [];

const wss = new WebSocketServer({
  port,
  host: "127.0.0.1",
  verifyClient: (info) => {
    try {
      const url = new URL(info.req.url, "http://127.0.0.1");
      return url.searchParams.get("token") === token;
    } catch {
      return false;
    }
  },
});

wss.on("connection", (ws) => {
  // A page reload/reconnect replaces whatever was previously connected --
  // only ever one real browser tab is the intended peer at a time. The
  // old socket (if any) is stale by definition once a new one authenticates.
  if (activeSocket && activeSocket !== ws) {
    activeSocket.terminate();
  }
  activeSocket = ws;

  while (pendingFromClaude.length > 0) {
    ws.send(Buffer.from(pendingFromClaude.shift(), "utf8"));
  }

  ws.on("message", (data) => {
    // Every reply/notification coming back from the browser's MCP server
    // goes straight to stdout, newline-delimited -- exactly what Claude
    // Desktop's own StdioClientTransport expects on the other end.
    process.stdout.write(data.toString() + "\n");
  });

  ws.on("close", () => {
    if (activeSocket === ws) activeSocket = null;
  });

  ws.on("error", (err) => {
    process.stderr.write(`usbridge mcp-bridge: websocket error: ${err.message}\n`);
  });
});

wss.on("error", (err) => {
  process.stderr.write(`usbridge mcp-bridge: failed to listen on 127.0.0.1:${port}: ${err.message}\n`);
  process.exit(1);
});

const rl = createInterface({ input: process.stdin, terminal: false });
rl.on("line", (line) => {
  if (activeSocket && activeSocket.readyState === activeSocket.OPEN) {
    // Binary frames, not text -- the browser side's Go/wasm WebSocket
    // wrapper (internal/platform/wsconn_wasm.go, shared with this app's
    // other WS-as-net.Conn uses) sets binaryType="arraybuffer" and only
    // handles binary frames on receive; a text frame would arrive on that
    // side as a JS string, not the ArrayBuffer it expects, and fail to
    // decode. UTF-8 JSON either way -- only the WebSocket-protocol framing
    // differs.
    activeSocket.send(Buffer.from(line, "utf8"));
  } else {
    pendingFromClaude.push(line);
  }
});

// Claude Desktop closes this process's stdin when it's shutting the
// server down (a config reload, or the app quitting) -- exiting cleanly
// here instead of lingering also releases the WebSocket port promptly for
// the next launch.
rl.on("close", () => {
  wss.close();
  process.exit(0);
});
