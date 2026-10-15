// video_sps_android.c — coded picture size from an H.264/HEVC SPS, so the
// Android decoder path can log what the host actually sends (a stream larger
// than the decoder was configured for fails on its first input buffer).
#ifdef __ANDROID__

#include <stddef.h>
#include <stdint.h>

typedef struct { const uint8_t *p; size_t n, bit; int bad; } vsps_br;

static uint32_t vsps_u(vsps_br *b, int bits) {
    uint32_t v = 0;
    while (bits-- > 0) {
        if (b->bit >= b->n * 8) { b->bad = 1; return 0; }
        v = (v << 1) | ((b->p[b->bit >> 3] >> (7 - (b->bit & 7))) & 1);
        b->bit++;
    }
    return v;
}

static uint32_t vsps_ue(vsps_br *b) {
    int zeros = 0;
    while (!b->bad && vsps_u(b, 1) == 0) if (++zeros > 31) { b->bad = 1; return 0; }
    return ((1u << zeros) - 1) + vsps_u(b, zeros);
}

static void vsps_skip(vsps_br *b, int bits) {
    b->bit += (size_t)bits;
    if (b->bit > b->n * 8) b->bad = 1;
}

// vsps_dims parses the SPS NAL unit at nal (header included, no start code).
// Returns 1 and the coded size (before cropping) on success.
int vsps_dims(const uint8_t *nal, size_t len, int hevc, int *w, int *h) {
    uint8_t rbsp[256];
    size_t n = 0;
    for (size_t i = 0; i < len && n < sizeof(rbsp); i++) {
        // Drop emulation-prevention bytes (00 00 03).
        if (i >= 2 && nal[i] == 3 && nal[i-1] == 0 && nal[i-2] == 0) continue;
        rbsp[n++] = nal[i];
    }
    vsps_br b = { rbsp, n, 0, 0 };
    if (hevc) {
        vsps_skip(&b, 16);              // NAL header
        vsps_skip(&b, 4);               // sps_video_parameter_set_id
        int sub = (int)vsps_u(&b, 3);   // sps_max_sub_layers_minus1
        vsps_skip(&b, 1);
        vsps_skip(&b, 2 + 1 + 5 + 32 + 48 + 8); // general profile_tier_level
        int prof[8] = {0}, lvl[8] = {0};
        for (int i = 0; i < sub; i++) { prof[i] = (int)vsps_u(&b, 1); lvl[i] = (int)vsps_u(&b, 1); }
        if (sub > 0) for (int i = sub; i < 8; i++) vsps_skip(&b, 2);
        for (int i = 0; i < sub; i++) {
            if (prof[i]) vsps_skip(&b, 88);
            if (lvl[i])  vsps_skip(&b, 8);
        }
        vsps_ue(&b);                    // sps_seq_parameter_set_id
        if (vsps_ue(&b) == 3) vsps_skip(&b, 1); // chroma_format_idc, separate_colour_plane
        *w = (int)vsps_ue(&b);
        *h = (int)vsps_ue(&b);
    } else {
        vsps_skip(&b, 8);               // NAL header
        int profile = (int)vsps_u(&b, 8);
        vsps_skip(&b, 16);
        vsps_ue(&b);                    // seq_parameter_set_id
        if (profile == 100 || profile == 110 || profile == 122 || profile == 244 || profile == 44 ||
            profile == 83 || profile == 86 || profile == 118 || profile == 128 || profile == 138 ||
            profile == 139 || profile == 134 || profile == 135) {
            if (vsps_ue(&b) == 3) vsps_skip(&b, 1);
            vsps_ue(&b); vsps_ue(&b);   // bit depths
            vsps_skip(&b, 1);
            if (vsps_u(&b, 1)) return 0; // scaling matrices: not worth parsing here
        }
        vsps_ue(&b);                    // log2_max_frame_num_minus4
        uint32_t poc = vsps_ue(&b);
        if (poc == 0) vsps_ue(&b);
        else if (poc == 1) {
            vsps_skip(&b, 1); vsps_ue(&b); vsps_ue(&b);
            uint32_t k = vsps_ue(&b);
            for (uint32_t i = 0; i < k && !b.bad; i++) vsps_ue(&b);
        }
        vsps_ue(&b);                    // max_num_ref_frames
        vsps_skip(&b, 1);
        int wmb = (int)vsps_ue(&b) + 1;
        int hmu = (int)vsps_ue(&b) + 1;
        int frame_mbs_only = (int)vsps_u(&b, 1);
        *w = wmb * 16;
        *h = (2 - frame_mbs_only) * hmu * 16;
    }
    return !b.bad && *w > 0 && *h > 0;
}

// ---- H.264 SPS fixup -------------------------------------------------------
// Without VUI bitstream_restriction a decoder must assume the stream may
// reorder up to its level's DPB size and holds that many frames before
// output: on Qualcomm (output.delay 16) and MediaTek this is 100+ ms. Host
// encoders never reorder, so like Moonlight-android we declare
// max_num_reorder_frames = 0 and max_dec_frame_buffering = max_num_ref_frames.

typedef struct { uint8_t *p; size_t cap, bit; int bad; } vsps_bw;

static void vsps_put(vsps_bw *w, uint32_t v, int bits) {
    while (bits-- > 0) {
        if (w->bit >= w->cap * 8) { w->bad = 1; return; }
        uint8_t *byte = &w->p[w->bit >> 3];
        int sh = 7 - (int)(w->bit & 7);
        *byte = (uint8_t)((*byte & ~(1u << sh)) | (((v >> bits) & 1u) << sh));
        w->bit++;
    }
}

static void vsps_put_ue(vsps_bw *w, uint32_t v) {
    uint32_t x = v + 1;
    int len = 0;
    for (uint32_t t = x; t > 1; t >>= 1) len++;
    vsps_put(w, 0, len);
    vsps_put(w, x, len + 1);
}

static int32_t vsps_se(vsps_br *b) {
    uint32_t k = vsps_ue(b);
    return (k & 1) ? (int32_t)((k + 1) / 2) : -(int32_t)(k / 2);
}

static void vsps_skip_scaling_list(vsps_br *b, int size) {
    int last = 8, next = 8;
    for (int j = 0; j < size && !b->bad; j++) {
        if (next != 0) next = (last + vsps_se(b) + 256) % 256;
        last = next == 0 ? last : next;
    }
}

static void vsps_skip_hrd(vsps_br *b) {
    uint32_t cnt = vsps_ue(b) + 1;
    vsps_skip(b, 8);
    for (uint32_t i = 0; i < cnt && !b->bad && i < 32; i++) { vsps_ue(b); vsps_ue(b); vsps_skip(b, 1); }
    vsps_skip(b, 20);
}

// vsps_h264_fixup rewrites the SPS NAL at nal (header included, no start
// code) into out with bitstream_restriction set. Returns the new length, or
// 0 when the SPS cannot be parsed (the caller keeps the original).
// *had_vui / *had_restr report what the host sent.
size_t vsps_h264_fixup(const uint8_t *nal, size_t len, uint8_t *out, size_t cap, int *had_vui, int *had_restr) {
    uint8_t rbsp[256];
    size_t n = 0;
    for (size_t i = 0; i < len; i++) {
        if (i >= 2 && nal[i] == 3 && nal[i-1] == 0 && nal[i-2] == 0) continue;
        if (n == sizeof(rbsp)) return 0;
        rbsp[n++] = nal[i];
    }
    vsps_br b = { rbsp, n, 0, 0 };
    vsps_skip(&b, 8);
    int profile = (int)vsps_u(&b, 8);
    vsps_skip(&b, 16);
    vsps_ue(&b);
    if (profile == 100 || profile == 110 || profile == 122 || profile == 244 || profile == 44 ||
        profile == 83 || profile == 86 || profile == 118 || profile == 128 || profile == 138 ||
        profile == 139 || profile == 134 || profile == 135) {
        uint32_t chroma = vsps_ue(&b);
        if (chroma == 3) vsps_skip(&b, 1);
        vsps_ue(&b); vsps_ue(&b);
        vsps_skip(&b, 1);
        if (vsps_u(&b, 1)) {
            int lists = chroma == 3 ? 12 : 8;
            for (int i = 0; i < lists && !b.bad; i++)
                if (vsps_u(&b, 1)) vsps_skip_scaling_list(&b, i < 6 ? 16 : 64);
        }
    }
    vsps_ue(&b);
    uint32_t poc = vsps_ue(&b);
    if (poc == 0) vsps_ue(&b);
    else if (poc == 1) {
        vsps_skip(&b, 1); vsps_se(&b); vsps_se(&b);
        uint32_t k = vsps_ue(&b);
        for (uint32_t i = 0; i < k && i < 256 && !b.bad; i++) vsps_se(&b);
    }
    uint32_t refs = vsps_ue(&b);
    vsps_skip(&b, 1);
    vsps_ue(&b); vsps_ue(&b);
    if (!vsps_u(&b, 1)) vsps_skip(&b, 1);   // frame_mbs_only, mb_adaptive
    vsps_skip(&b, 1);                        // direct_8x8_inference
    if (vsps_u(&b, 1)) { vsps_ue(&b); vsps_ue(&b); vsps_ue(&b); vsps_ue(&b); }
    size_t vui_flag_pos = b.bit;
    int vui = (int)vsps_u(&b, 1);
    size_t keep = 0; // bits copied verbatim from rbsp
    if (vui) {
        if (vsps_u(&b, 1) && vsps_u(&b, 8) == 255) vsps_skip(&b, 32);
        if (vsps_u(&b, 1)) vsps_skip(&b, 1);
        if (vsps_u(&b, 1)) { vsps_skip(&b, 4); if (vsps_u(&b, 1)) vsps_skip(&b, 24); }
        if (vsps_u(&b, 1)) { vsps_ue(&b); vsps_ue(&b); }
        if (vsps_u(&b, 1)) vsps_skip(&b, 65);
        int nal_hrd = (int)vsps_u(&b, 1);
        if (nal_hrd) vsps_skip_hrd(&b);
        int vcl_hrd = (int)vsps_u(&b, 1);
        if (vcl_hrd) vsps_skip_hrd(&b);
        if (nal_hrd || vcl_hrd) vsps_skip(&b, 1);
        vsps_skip(&b, 1);                    // pic_struct_present
        keep = b.bit;
        *had_restr = (int)vsps_u(&b, 1);
    } else {
        keep = vui_flag_pos;
        *had_restr = 0;
    }
    *had_vui = vui;
    if (b.bad) return 0;

    uint8_t wbuf[300] = {0};
    vsps_bw w = { wbuf, sizeof(wbuf), 0, 0 };
    vsps_br c = { rbsp, n, 0, 0 };
    for (size_t i = 0; i < keep; i++) vsps_put(&w, vsps_u(&c, 1), 1);
    if (!vui) {
        vsps_put(&w, 1, 1);  // vui_parameters_present_flag
        vsps_put(&w, 0, 8);  // aspect, overscan, signal, chroma_loc, timing, nal/vcl hrd, pic_struct
    }
    vsps_put(&w, 1, 1);      // bitstream_restriction_flag
    vsps_put(&w, 1, 1);      // motion_vectors_over_pic_boundaries
    vsps_put_ue(&w, 2);      // max_bytes_per_pic_denom
    vsps_put_ue(&w, 1);      // max_bits_per_mb_denom
    vsps_put_ue(&w, 16);     // log2_max_mv_length_horizontal
    vsps_put_ue(&w, 16);     // log2_max_mv_length_vertical
    vsps_put_ue(&w, 0);      // max_num_reorder_frames
    vsps_put_ue(&w, refs > 0 ? refs : 1); // max_dec_frame_buffering
    vsps_put(&w, 1, 1);      // rbsp_stop_one_bit
    while (w.bit & 7) vsps_put(&w, 0, 1);
    if (w.bad) return 0;

    size_t wn = w.bit / 8, o = 0;
    int zeros = 0;
    for (size_t i = 0; i < wn; i++) {
        if (zeros >= 2 && wbuf[i] <= 3) {
            if (o == cap) return 0;
            out[o++] = 3;
            zeros = 0;
        }
        if (o == cap) return 0;
        out[o++] = wbuf[i];
        zeros = wbuf[i] == 0 ? zeros + 1 : 0;
    }
    return o;
}

#endif // __ANDROID__
