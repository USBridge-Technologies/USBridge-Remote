// net_graph_stats_windows.c — do_get_rtp_video_stats / do_get_estimated_rtt_info /
// do_get_last_host_latency_tenths_ms / do_get_playout_jitter_us /
// do_get_playout_applied_delay_us for net_graph_windows.go.
//
// These live in their own translation unit rather than inline in
// moonlight_cgo_windows.go's cgo preamble comment because that file also
// has //export directives: cgo duplicates any non-static function *body*
// written directly in a preamble into the generated _cgo_export.c (needed
// there to get type declarations right for //export'd Go functions), and
// linking then fails with "multiple definition". Functions defined in a
// plain .c file in the package don't have this problem — see
// vk_hwdev_bridge_windows.c for the same pattern already used here.

#ifdef _WIN32

#include <stdint.h>
#include <Limelight.h>

// Defined here (not just declared) -- see moonlight_cgo_windows.go's comment
// next to its extern declaration of this symbol for why.
volatile uint16_t g_last_host_latency_tenths_ms = 0;

// g_last_decode_ms: most recent win_deliver_frame/win_deliver_frame_vulkan
// call's wall time (moonlight_cgo_windows.go), i.e. decode + (on the
// zero-copy path) HUD/AI-Vision GPU work + submit -- the closest Windows
// equivalent to metal_video_impl_darwin.m's "submit-to-display" decode
// latency stat. Written from moonlight_cgo_windows.go's own translation
// unit (extern declaration there, same reasoning as
// g_last_host_latency_tenths_ms above); read here by
// win_get_last_decode_ms for net_graph_windows.go's GetDecodeMs.
volatile double g_last_decode_ms = 0.0;

double win_get_last_decode_ms(void) {
    return g_last_decode_ms;
}

void do_get_rtp_video_stats(uint32_t *out) {
    const RTP_VIDEO_STATS *stats = LiGetRTPVideoStats();
    out[0] = stats->packetCountVideo;
    out[1] = stats->packetCountFec;
    out[2] = stats->packetCountFecRecovered;
    out[3] = stats->packetCountFecFailed;
    out[4] = stats->packetCountOOS;
    out[5] = stats->packetCountInvalid;
    out[6] = stats->packetCountFecInvalid;
}

int do_get_estimated_rtt_info(uint32_t *out) {
    uint32_t rtt = 0, rttVariance = 0;
    int ok = LiGetEstimatedRttInfo(&rtt, &rttVariance) ? 1 : 0;
    out[0] = rtt;
    out[1] = rttVariance;
    return ok;
}

uint16_t do_get_last_host_latency_tenths_ms(void) {
    return g_last_host_latency_tenths_ms;
}

uint64_t do_get_playout_jitter_us(void) {
    return LiGetPlayoutJitterUs();
}

uint64_t do_get_playout_applied_delay_us(void) {
    return LiGetPlayoutAppliedDelayUs();
}

#endif // _WIN32
