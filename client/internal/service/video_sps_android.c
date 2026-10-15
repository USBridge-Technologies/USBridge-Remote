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

#endif // __ANDROID__
