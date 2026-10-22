// net_graph_stats_android.c — do_get_rtp_video_stats / do_get_estimated_rtt_info /
// do_get_last_host_latency_tenths_ms / do_get_playout_jitter_us /
// do_get_playout_applied_delay_us for net_graph_android.go.
//
// Same reason as net_graph_stats_windows.c: moonlight_cgo_android.go has
// //export directives, so a non-static function body in its cgo preamble
// would be duplicated into _cgo_export.c and fail to link.

#ifdef __ANDROID__

#include <stdint.h>
#include <Limelight.h>

volatile uint16_t g_last_host_latency_tenths_ms = 0;

// VIDEO_FORMAT_* the running decoder was set up for (amc_init), -1 when no
// stream is decoding -- Net Graph's codec field.
volatile int g_amc_negotiated_fmt = -1;
int do_get_negotiated_video_format(void) { return g_amc_negotiated_fmt; }

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

// Android paces at the display (vk_video_impl_android.c), not in
// moonlight-common-c, so BUF is the renderer's hold; the jitter estimate is
// still the depacketizer's. (These used to return 0: the Android .a was never
// rebuilt and lacked the exports -- build_moonlight.sh now rebuilds it.)
extern uint64_t android_vk_pace_hold_us(void);

uint64_t do_get_playout_jitter_us(void) {
    return LiGetPlayoutJitterUs();
}

uint64_t do_get_playout_applied_delay_us(void) {
    return android_vk_pace_hold_us();
}

#endif // __ANDROID__
