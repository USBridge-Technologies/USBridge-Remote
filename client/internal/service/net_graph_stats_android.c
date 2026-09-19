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

// LiGetPlayoutJitterUs / LiGetPlayoutAppliedDelayUs exist in
// VideoDepacketizer.c and Limelight.h, but the cached Android
// libmoonlight-common-c.a (build_moonlight.sh skips rebuild if the .a
// already exists) predates those exports. A strong reference here
// survives into libUSBridge_Client.so and Android's loader aborts on
// dlopen with UnsatisfiedLinkError -- the app never even reaches main.
// Return 0 until that .a is rebuilt; RTP/RTT/FPS still populate the HUD.
uint64_t do_get_playout_jitter_us(void) {
    return 0;
}

uint64_t do_get_playout_applied_delay_us(void) {
    return 0;
}

#endif // __ANDROID__
