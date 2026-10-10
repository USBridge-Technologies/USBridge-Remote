// The browser half of rust-shine's WebTransport stream (its
// crates/webrtc-video/src/webtransport.rs has the protocol): H.264 access
// units arrive one per unidirectional stream and go through WebCodecs into
// a MediaStreamTrackGenerator, so the page's ordinary <video> element (and
// everything wt_wasm.go's callers already do with it) shows them; Opus
// datagrams take the same route into an <audio>. Kept in JS rather than Go
// because it runs per frame and per packet. wt_wasm.go evaluates this once
// and drives window.__usbridgeWT.
(function () {
  if (window.__usbridgeWT) return;

  const MSG_HELLO = 1, MSG_INPUT = 2, MSG_KEYFRAME = 3, MSG_OPEN = 4, MSG_DATA = 5, MSG_OPENED = 6;
  const MSG_WELCOME = 1;
  // The server's last word when another page opened a session: this page
  // must not reconnect on its own, or two tabs take the stream from each
  // other forever.
  const MSG_TAKEN_OVER = 7;
  const KIND_VIDEO = 1, KIND_AUDIO = 2;
  // Decoder queue depth past which a delta frame is dropped and the stream
  // resumes at a keyframe instead: latency over smoothness.
  const MAX_DECODE_QUEUE = 4;
  const enc = new TextEncoder(), dec = new TextDecoder();

  function supported() {
    return !!(window.isSecureContext && typeof WebTransport === 'function' && typeof VideoDecoder === 'function' &&
      typeof EncodedVideoChunk === 'function' && typeof MediaStreamTrackGenerator === 'function');
  }

  function frame(type, payload) {
    const out = new Uint8Array(5 + payload.length);
    out[0] = type;
    new DataView(out.buffer).setUint32(1, payload.length);
    out.set(payload, 5);
    return out;
  }

  // Splits a byte stream into [type u8][len u32][payload] messages.
  function messageReader(readable, onMessage) {
    const r = readable.getReader();
    let buf = new Uint8Array(0);
    return (async () => {
      for (;;) {
        const { value, done } = await r.read();
        if (done) return;
        const b = new Uint8Array(buf.length + value.length);
        b.set(buf); b.set(value, buf.length); buf = b;
        while (buf.length >= 5) {
          const len = new DataView(buf.buffer, buf.byteOffset).getUint32(1);
          if (buf.length < 5 + len) break;
          onMessage(buf[0], buf.slice(5, 5 + len));
          buf = buf.subarray(5 + len);
        }
      }
    })();
  }

  async function readAll(stream) {
    const r = stream.getReader();
    const parts = [];
    let n = 0;
    for (;;) {
      const { value, done } = await r.read();
      if (done) break;
      parts.push(value); n += value.length;
    }
    if (parts.length === 1) return parts[0];
    const out = new Uint8Array(n);
    let o = 0;
    for (const p of parts) { out.set(p, o); o += p.length; }
    return out;
  }

  // avc1.PPCCLL from the first SPS in an Annex B access unit.
  function avcCodec(au) {
    for (let i = 0; i + 6 < au.length; i++) {
      if (au[i] === 0 && au[i + 1] === 0 && au[i + 2] === 1 && (au[i + 3] & 0x1f) === 7) {
        let s = 'avc1.';
        for (let k = 4; k < 7; k++) s += au[i + k].toString(16).padStart(2, '0').toUpperCase();
        return s;
      }
    }
    return null;
  }

  // HEVC Main, level 5.1: one codec string for both the support probe and
  // the decoder (the Annex B stream carries its own VPS/SPS/PPS).
  const HEVC_CODEC = 'hvc1.1.6.L153.B0';

  async function hevcSupported() {
    if (typeof VideoDecoder !== 'function' || !VideoDecoder.isConfigSupported) return false;
    try {
      const r = await VideoDecoder.isConfigSupported({ codec: HEVC_CODEC, optimizeForLatency: true });
      return !!r.supported;
    } catch (_) {
      return false;
    }
  }

  // Video decode and paint run in a dedicated worker on an OffscreenCanvas
  // when the browser can: a VideoDecoder's output and its queue are served
  // by its own thread's event loop, so on the page's thread every long
  // task of the UI (Fyne) looked like a decoder backlog -- frames dropped,
  // a keyframe asked for, the stream ballooning, 60 fps sagging to 20.
  // The page now only hands each access unit over; after a stall the worker
  // decodes the burst and paints the newest frame.
  const WORKER_SRC = `'use strict';
const HEVC = ${JSON.stringify(HEVC_CODEC)};
// A real decoder backlog (~200 ms at 60 fps), not a busy page.
const MAX_QUEUE = 12;
let canvas, ctx, h265 = false, decoder = null, configured = '', waitingKey = true;
// The GPU's decoder, asked for outright; software only when the browser
// has none for this stream (a configure error), and then said so.
let accel = 'prefer-hardware';
const st = { framesDecoded: 0, framesDropped: 0, decodeTimeMs: 0, frames: 0, w: 0, h: 0, decoder: '' };
const started = new Map();
function report() { postMessage({ type: 'stats', st }); }
setInterval(report, 250);
${avcCodec.toString()}
// Every frame is painted as it's decoded: the compositor shows the newest
// one at each vsync. Pacing paints with the worker's requestAnimationFrame
// painted only about half of a 60 fps stream.
function draw(f) {
  if (canvas.width !== f.displayWidth || canvas.height !== f.displayHeight) {
    canvas.width = f.displayWidth; canvas.height = f.displayHeight;
    st.w = f.displayWidth; st.h = f.displayHeight;
    report();
  }
  ctx.drawImage(f, 0, 0);
  f.close();
  st.frames++;
  // Per frame: the page's frame counter reads at most a few new frames per
  // poll, so a 250 ms report capped it near 30 fps.
  postMessage({ type: 'frame', n: st.frames });
}
function keyframe() { postMessage({ type: 'keyframe' }); }
function newDecoder() {
  return new VideoDecoder({
    output(f) {
      const t0 = started.get(f.timestamp);
      if (t0 !== undefined) { st.decodeTimeMs += performance.now() - t0; started.delete(f.timestamp); }
      st.framesDecoded++;
      draw(f);
    },
    error(e) {
      if (accel === 'prefer-hardware' && st.framesDecoded === 0) {
        accel = 'no-preference';
        postMessage({ type: 'log', msg: 'no hardware decoder for ' + configured + ' (' + (e && e.message) + '), decoding in software' });
      }
      configured = ''; waitingKey = true; started.clear(); keyframe();
    },
  });
}
function onAU(au) {
  const key = (au[1] & 1) === 1;
  const ts = Number(new DataView(au.buffer, au.byteOffset).getBigUint64(2));
  const data = au.subarray(10);
  if (waitingKey && !key) { st.framesDropped++; return; }
  if (!decoder || decoder.state === 'closed') { decoder = newDecoder(); configured = ''; }
  if (key) {
    const codec = h265 ? HEVC : (avcCodec(data) || 'avc1.42E01F');
    if (codec !== configured) {
      decoder.configure({ codec, optimizeForLatency: true, hardwareAcceleration: accel });
      configured = codec;
      st.decoder = accel === 'prefer-hardware' ? 'hardware' : 'software';
    }
  }
  if (!configured) { st.framesDropped++; waitingKey = true; return; }
  waitingKey = false;
  if (!key && decoder.decodeQueueSize > MAX_QUEUE) { st.framesDropped++; waitingKey = true; keyframe(); return; }
  started.set(ts, performance.now());
  if (started.size > 64) started.clear();
  try {
    decoder.decode(new EncodedVideoChunk({ type: key ? 'key' : 'delta', timestamp: ts, data }));
  } catch (_) {
    configured = ''; waitingKey = true; keyframe();
  }
}
onmessage = (e) => {
  const m = e.data;
  if (m.type === 'init') {
    canvas = m.canvas; ctx = canvas.getContext('2d', { alpha: false }); h265 = m.h265;
    // Ask before the first keyframe, so a browser without a GPU decoder
    // (Linux Chrome without VA-API) starts in software instead of losing
    // the first keyframe to a failed configure.
    VideoDecoder.isConfigSupported({ codec: h265 ? HEVC : 'avc1.640034', optimizeForLatency: true, hardwareAcceleration: 'prefer-hardware' })
      .then((r) => {
        if (r.supported || accel !== 'prefer-hardware') return;
        accel = 'no-preference';
        postMessage({ type: 'log', msg: 'this browser has no hardware ' + (h265 ? 'HEVC' : 'H.264') + ' decoder, decoding in software' });
      }, () => {});
  }
  else if (m.type === 'au') onAU(new Uint8Array(m.buf));
  else if (m.type === 'close') {
    try { if (decoder && decoder.state !== 'closed') decoder.close(); } catch (_) {}
    close();
  }
};
`;

  // videoSurface returns what the page shows the video in: a <canvas>
  // taking videoEl's place, painted by a worker (see WORKER_SRC), or
  // videoEl itself where workers can't do that. The canvas carries what the
  // Go side reads off a <video>: videoWidth/videoHeight and
  // getVideoPlaybackQuality().totalVideoFrames.
  function videoSurface(videoEl) {
    if (typeof Worker !== 'function' || typeof OffscreenCanvas !== 'function' || typeof VideoDecoder !== 'function' ||
      typeof HTMLCanvasElement.prototype.transferControlToOffscreen !== 'function') return videoEl;
    let worker;
    try {
      worker = new Worker(URL.createObjectURL(new Blob([WORKER_SRC], { type: 'text/javascript' })));
    } catch (_) {
      return videoEl; // e.g. a CSP without blob: workers
    }
    const c = document.createElement('canvas');
    c.id = videoEl.id;
    c.style.cssText = videoEl.style.cssText;
    if (videoEl.parentNode) videoEl.replaceWith(c);
    c.videoWidth = 0;
    c.videoHeight = 0;
    c.__frames = 0;
    c.getVideoPlaybackQuality = () => ({ totalVideoFrames: c.__frames, droppedVideoFrames: 0 });
    c.__worker = worker;
    return c;
  }

  function hexBytes(hex) {
    const out = new Uint8Array(hex.length / 2);
    for (let i = 0; i < out.length; i++) out[i] = parseInt(hex.substr(i * 2, 2), 16);
    return out;
  }

  // A named channel over one bidirectional stream, shaped like the parts of
  // RTCDataChannel that dcconn_wasm.go uses.
  class Channel extends EventTarget {
    constructor(session, label) {
      super();
      this.label = label;
      this.binaryType = 'arraybuffer';
      this.readyState = 'connecting';
      this._writer = null;
      session._wt.createBidirectionalStream().then(async (bidi) => {
        this._writer = bidi.writable.getWriter();
        await this._writer.write(frame(MSG_OPEN, enc.encode(label)));
        // Open only once the host confirms its end; a stream that ends
        // first is a channel that couldn't be attached.
        await messageReader(bidi.readable, (type, payload) => {
          if (type === MSG_OPENED && this.readyState === 'connecting') {
            this.readyState = 'open';
            this.dispatchEvent(new Event('open'));
            return;
          }
          if (type !== MSG_DATA || this.readyState !== 'open') return;
          const ev = new Event('message');
          ev.data = payload.buffer.slice(payload.byteOffset, payload.byteOffset + payload.byteLength);
          this.dispatchEvent(ev);
        });
        if (this.readyState === 'connecting') this.dispatchEvent(new Event('error'));
        this._closed();
      }).catch(() => {
        if (this.readyState === 'connecting') this.dispatchEvent(new Event('error'));
        this._closed();
      });
    }
    send(data) {
      if (this.readyState !== 'open') throw new Error('channel not open');
      const bytes = typeof data === 'string' ? enc.encode(data) :
        data instanceof ArrayBuffer ? new Uint8Array(data) : new Uint8Array(data.buffer, data.byteOffset, data.byteLength);
      this._writer.write(frame(MSG_DATA, bytes)).catch(() => this._closed());
    }
    close() {
      if (this._writer) this._writer.close().catch(() => {});
      this._closed();
    }
    _closed() {
      if (this.readyState === 'closed') return;
      this.readyState = 'closed';
      this.dispatchEvent(new Event('close'));
    }
  }

  class Session {
    // opts: { url, certHash, hello (object), videoEl, audioEl, onState(state, detail) }
    constructor(opts) {
      this._opts = opts;
      this.welcome = null;
      this.stats = { framesReceived: 0, framesDecoded: 0, framesDropped: 0, keyframes: 0, bytesReceived: 0,
        audioPackets: 0, decodeErrors: 0, decodeTimeMs: 0, rttMs: 0, codec: '' };
      this._closedFlag = false;
    }

    async open(timeoutMs) {
      const o = this._opts;
      const wt = this._wt = new WebTransport(o.url, {
        serverCertificateHashes: [{ algorithm: 'sha-256', value: hexBytes(o.certHash) }],
        congestionControl: 'low-latency',
      });
      let timer;
      const timeout = new Promise((_, reject) => { timer = setTimeout(() => reject(new Error('WebTransport: connect timed out')), timeoutMs); });
      try {
        await Promise.race([wt.ready, timeout]);
        const ctl = await wt.createBidirectionalStream();
        this._ctl = ctl.writable.getWriter();
        await this._ctl.write(frame(MSG_HELLO, enc.encode(JSON.stringify(o.hello || {}))));
        const welcome = new Promise((resolve) => {
          messageReader(ctl.readable, (type, payload) => {
            if (type === MSG_WELCOME) resolve(JSON.parse(dec.decode(payload)));
            else if (type === MSG_TAKEN_OVER) { this._end('taken-over'); try { wt.close(); } catch (_) {} }
          }).catch(() => {});
        });
        this.welcome = await Promise.race([welcome, timeout]);
      } catch (e) {
        try { wt.close(); } catch (_) {}
        if (o.videoEl && o.videoEl.__worker) o.videoEl.__worker.terminate();
        throw e;
      } finally {
        clearTimeout(timer);
      }
      this.stats.codec = this.welcome.codec || 'h264';
      window.__usbridgeWTLast = { hello: o.hello, welcome: this.welcome };

      wt.closed.then(() => this._end('closed'), (e) => this._end('failed', String(e && e.message || e)));
      this._startVideo();
      this._startAudio();
      this._statsTimer = setInterval(() => {
        if (typeof wt.getStats !== 'function') return;
        wt.getStats().then((s) => { const rtt = s && (s.smoothedRtt || s.latestRtt || s.minRtt); if (rtt) this.stats.rttMs = rtt; }).catch(() => {});
      }, 1000);
    }

    _startVideo() {
      const el = this._opts.videoEl;
      const decode = el.__worker ? this._workerVideo(el) : this._elementVideo(el);
      const onAU = (au) => {
        if (au.length < 10 || au[0] !== KIND_VIDEO) return;
        this.stats.framesReceived++;
        this.stats.bytesReceived += au.length;
        if ((au[1] & 1) === 1) this.stats.keyframes++;
        decode(au);
      };

      // Streams are accepted in order: read them concurrently, decode in order.
      (async () => {
        const r = this._wt.incomingUnidirectionalStreams.getReader();
        let chain = Promise.resolve();
        for (;;) {
          let next;
          try { next = await r.read(); } catch (_) { return; }
          if (next.done) return;
          const body = readAll(next.value).catch(() => null);
          chain = chain.then(async () => {
            const au = await body;
            if (au && !this._closedFlag) onAU(au);
          });
        }
      })();
    }

    // The worker path (see videoSurface): access units go over, stats and
    // keyframe requests come back.
    _workerVideo(canvas) {
      const w = this._worker = canvas.__worker;
      const off = canvas.transferControlToOffscreen();
      w.postMessage({ type: 'init', canvas: off, h265: this.welcome.codec === 'h265' }, [off]);
      w.onmessage = (e) => {
        const m = e.data;
        if (m.type === 'keyframe') { this.requestKeyframe(); return; }
        if (m.type === 'log') { console.warn('[webrtc-video] ' + m.msg); return; }
        if (m.type === 'frame') { canvas.__frames = m.n; return; }
        if (m.type !== 'stats') return;
        const st = m.st;
        this.stats.framesDecoded = st.framesDecoded;
        this.stats.framesDropped = st.framesDropped;
        this.stats.decodeTimeMs = st.decodeTimeMs;
        this.stats.decoder = st.decoder;
        canvas.__frames = st.frames;
        canvas.videoWidth = st.w;
        canvas.videoHeight = st.h;
      };
      return (au) => {
        // Hand the bytes over, not a copy.
        const buf = au.byteOffset === 0 && au.byteLength === au.buffer.byteLength ? au.buffer : au.slice().buffer;
        w.postMessage({ type: 'au', buf }, [buf]);
      };
    }

    // In-page decode into the <video> (no worker support).
    _elementVideo(videoEl) {
      const gen = this._videoGen = new MediaStreamTrackGenerator({ kind: 'video' });
      const writer = gen.writable.getWriter();
      videoEl.srcObject = new MediaStream([gen]);
      const play = videoEl.play();
      if (play) play.catch(() => {});

      let configured = '', waitingKey = true;
      const pending = new Map(); // timestamp -> decode start, for decode time
      // A decode error closes the decoder; a fresh one picks up at the next
      // keyframe.
      const newDecoder = () => new VideoDecoder({
        output: (f) => {
          const t0 = pending.get(f.timestamp);
          if (t0 !== undefined) { this.stats.decodeTimeMs += performance.now() - t0; pending.delete(f.timestamp); }
          this.stats.framesDecoded++;
          writer.write(f).catch(() => f.close());
        },
        error: () => {
          this.stats.decodeErrors++;
          configured = ''; waitingKey = true;
          pending.clear();
          this.requestKeyframe();
        },
      });
      this._decoder = newDecoder();

      return (au) => {
        const key = (au[1] & 1) === 1;
        const ts = Number(new DataView(au.buffer, au.byteOffset).getBigUint64(2));
        const data = au.subarray(10);
        if (waitingKey && !key) { this.stats.framesDropped++; return; }
        if (this._decoder.state === 'closed') { this._decoder = newDecoder(); configured = ''; }
        const d = this._decoder;
        if (key) {
          const codec = this.welcome.codec === 'h265' ? HEVC_CODEC : (avcCodec(data) || 'avc1.42E01F');
          if (codec !== configured) {
            d.configure({ codec, optimizeForLatency: true });
            configured = codec;
          }
        }
        if (!configured) { this.stats.framesDropped++; waitingKey = true; return; }
        waitingKey = false;
        if (!key && d.decodeQueueSize > MAX_DECODE_QUEUE) {
          this.stats.framesDropped++;
          waitingKey = true;
          this.requestKeyframe();
          return;
        }
        pending.set(ts, performance.now());
        if (pending.size > 64) pending.clear();
        try {
          d.decode(new EncodedVideoChunk({ type: key ? 'key' : 'delta', timestamp: ts, data }));
        } catch (_) {
          configured = ''; waitingKey = true;
          this.requestKeyframe();
        }
      };
    }

    _startAudio() {
      if (typeof AudioDecoder !== 'function' || !this._opts.audioEl) return;
      const channels = (this.welcome && this.welcome.audio_channels) || 2;
      const gen = this._audioGen = new MediaStreamTrackGenerator({ kind: 'audio' });
      const writer = gen.writable.getWriter();
      const audioEl = this._opts.audioEl;
      audioEl.srcObject = new MediaStream([gen]);
      const play = audioEl.play();
      if (play) play.catch(() => {});
      const ad = this._audioDecoder = new AudioDecoder({
        output: (a) => { writer.write(a).catch(() => a.close()); },
        error: () => {},
      });
      ad.configure({ codec: 'opus', sampleRate: 48000, numberOfChannels: channels });
      (async () => {
        const r = this._wt.datagrams.readable.getReader();
        for (;;) {
          let next;
          try { next = await r.read(); } catch (_) { return; }
          if (next.done) return;
          const d = next.value;
          if (d.length < 9 || d[0] !== KIND_AUDIO || ad.state !== 'configured') continue;
          this.stats.audioPackets++;
          const ts = Number(new DataView(d.buffer, d.byteOffset).getBigUint64(1));
          // Behind (tab was throttled): drop rather than build up delay.
          if (ad.decodeQueueSize > 10) continue;
          try { ad.decode(new EncodedAudioChunk({ type: 'key', timestamp: ts, data: d.subarray(9) })); } catch (_) {}
        }
      })();
    }

    sendInput(bytes) {
      if (!this._ctl || this._closedFlag) return false;
      this._ctl.write(frame(MSG_INPUT, bytes)).catch(() => {});
      return true;
    }

    requestKeyframe() {
      if (!this._ctl || this._closedFlag) return;
      this._ctl.write(frame(MSG_KEYFRAME, new Uint8Array(0))).catch(() => {});
    }

    createDataChannel(label) { return new Channel(this, label); }

    getStats() { return Object.assign({}, this.stats); }

    _end(state, detail) {
      if (this._closedFlag) return;
      this._closedFlag = true;
      clearInterval(this._statsTimer);
      for (const d of [this._decoder, this._audioDecoder]) { try { if (d && d.state !== 'closed') d.close(); } catch (_) {} }
      for (const g of [this._videoGen, this._audioGen]) { try { if (g) g.stop(); } catch (_) {} }
      if (this._worker) { this._worker.postMessage({ type: 'close' }); this._worker = null; }
      const cb = this._opts.onState;
      if (cb) cb(state, detail || '');
    }

    close() {
      try { this._wt.close(); } catch (_) {}
      this._end('closed');
    }
  }

  window.__usbridgeWT = {
    supported,
    hevcSupported,
    videoSurface,
    // Resolves with an open Session, or rejects (unreachable, refused,
    // timed out) so the caller can fall back to WebRTC.
    open: async (opts, timeoutMs) => {
      const s = new Session(opts);
      await s.open(timeoutMs || 5000);
      return s;
    },
  };
})();
