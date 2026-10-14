// PyroWave frame framing -> the one dense access unit the decoders push.
//
// The USBridge streamers (rust-shine, Punktfunk) send a frame as one dense PyroWave packet: a
// BitstreamSequenceHeader followed by block records. Vibepollo (Nonary) sends the same records
// in one of two wrappers, chosen by what the client announced (docs/pyrowave-protocol.md in
// its repository):
//
//   record framing  : the records themselves, with padding records between them
//                     (0xFFFFFFFF, N, N zero words) that keep records payload-aligned;
//   length-prefixed : [u32 LE packet_count] { [u32 LE size] [size bytes] } * packet_count,
//                     each packet one `pyrowave_encoder_packetize` output (packet 0 starts
//                     with the sequence header).
//
// Both reduce to the dense form by dropping the wrapper and the padding; the per-record
// layout is BitstreamHeader / BitstreamSequenceHeader (pyrowave_common.hpp):
// a block record's first word has `payload_words` (header included) in bits 16..27 and a
// clear `extended` bit 31; a sequence header is two words with bit 31 set.
#pragma once

#include <stddef.h>
#include <stdint.h>
#include <string.h>

static inline uint32_t pwf_le32(const uint8_t *p) {
    return (uint32_t)p[0] | (uint32_t)p[1] << 8 | (uint32_t)p[2] << 16 | (uint32_t)p[3] << 24;
}

// pw_unframe writes the dense access unit for the frame payload (in, len) to out (room for
// len bytes) and returns its length; 0 when the payload is malformed (the frame is dropped,
// the next one is independent).
static inline size_t pw_unframe(const uint8_t *in, size_t len, uint8_t *out) {
    if (len < 8) return 0;
    size_t o = 0;
    if (pwf_le32(in) >> 31) {
        // Dense or record framed: copy the records, skip padding.
        size_t i = 0;
        while (i + 4 <= len) {
            uint32_t w = pwf_le32(in + i);
            size_t sz;
            if (w == 0xFFFFFFFFu) {
                if (i + 8 > len) break;
                uint32_t n = pwf_le32(in + i + 4);
                sz = 8 + (size_t)n * 4;
                if (sz > len - i) break; // padding past the end: only a tail, nothing to keep
                i += sz;
                continue;
            }
            sz = (w >> 31) ? 8 : (size_t)((w >> 16) & 0xfff) * 4;
            if (sz < 8 || sz > len - i) return 0;
            memcpy(out + o, in + i, sz);
            o += sz;
            i += sz;
        }
        return o;
    }
    // Length-prefixed: a packet count (never has bit 31 set), then size-prefixed packets.
    uint32_t count = pwf_le32(in);
    if (count == 0 || count > 8192) return 0;
    size_t i = 4;
    for (uint32_t k = 0; k < count; k++) {
        if (i + 4 > len) return 0;
        size_t sz = pwf_le32(in + i);
        i += 4;
        if (sz > len - i) return 0;
        memcpy(out + o, in + i, sz);
        o += sz;
        i += sz;
    }
    return o;
}
