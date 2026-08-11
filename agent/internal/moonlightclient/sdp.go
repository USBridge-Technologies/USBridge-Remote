package moonlightclient

import "fmt"

// buildSDP constructs the SDP payload the RTSP ANNOUNCE request carries,
// telling Sunshine how to configure the encoder/streamer before it starts
// sending RTP. Reverse-derived from moonlight-common-c's SdpGenerator.c
// (getSdpPayloadForStreamConfig / getAttributesList / addGen5Options),
// trimmed to exactly the attributes relevant to a Sunshine host running the
// modern (gen7, APP_VERSION_AT_LEAST(7,1,431)) protocol -- the only variant
// this package targets, since it only ever talks to Sunshine, never GFE.
//
// Deliberately NOT requesting: video encryption (SS_ENC_VIDEO), audio
// encryption (SS_ENC_AUDIO), or control-v2 encryption (SS_ENC_CONTROL_V2) --
// set via "x-ss-general.encryptionEnabled:0" below. webrtcbridge wants to
// read raw, unencrypted RTP straight off the loopback UDP sockets, and
// control.go's ENet framing only implements the "legacy" (non-V2) IV scheme
// for the control channel (which Sunshine always encrypts regardless of
// this flag, just with a simpler per-message IV when V2 isn't requested) --
// see control.go's doc comment. NVFF_RI_ENCRYPTION in featureFlags below is
// unrelated and always set (Sunshine expects it), but remote input is never
// actually sent by this package.
func buildSDP(rtspClientVersion int, host string, videoPort, width, height, fps, bitrateKbps int) string {
	// x-nv-general.featureFlags: NVFF_BASE(0x07) | NVFF_RI_ENCRYPTION(0x80).
	const nvFeatureFlags = 0x07 | 0x80
	// x-ml-general.featureFlags: ML_FF_FEC_STATUS(0x01) | ML_FF_SESSION_ID_V1(0x02)
	// -- tells Sunshine we understand its X-SS-Ping-Payload/X-SS-Connect-Data
	// RTSP SETUP extensions. We don't send FEC status control messages
	// ourselves (see control.go), but advertising support is harmless.
	const mlFeatureFlags = 0x01 | 0x02

	adjustedBitrate := int(float64(bitrateKbps) * 0.80)
	if adjustedBitrate > 100000 {
		adjustedBitrate = 100000
	}

	attrs := fmt.Sprintf(
		"a=x-nv-general.featureFlags:%d \r\n"+
			"a=x-nv-general.useReliableUdp:13 \r\n"+
			"a=x-ml-general.featureFlags:%d \r\n"+
			"a=x-ss-general.encryptionEnabled:0 \r\n"+
			"a=x-ss-video[0].chromaSamplingType:0 \r\n"+
			"a=x-nv-video[0].clientViewportWd:%d \r\n"+
			"a=x-nv-video[0].clientViewportHt:%d \r\n"+
			"a=x-nv-video[0].maxFPS:%d \r\n"+
			"a=x-nv-video[0].packetSize:1024 \r\n"+
			"a=x-nv-video[0].rateControlMode:4 \r\n"+
			"a=x-nv-video[0].timeoutLengthMs:7000 \r\n"+
			"a=x-nv-video[0].framesWithInvalidRefThreshold:0 \r\n"+
			"a=x-nv-video[0].initialBitrateKbps:%d \r\n"+
			"a=x-nv-video[0].initialPeakBitrateKbps:%d \r\n"+
			"a=x-nv-vqos[0].bw.minimumBitrateKbps:%d \r\n"+
			"a=x-nv-vqos[0].bw.maximumBitrateKbps:%d \r\n"+
			"a=x-ml-video.configuredBitrateKbps:%d \r\n"+
			"a=x-nv-vqos[0].fec.minRequiredFecPackets:2 \r\n"+
			"a=x-nv-vqos[0].bllFec.enable:0 \r\n"+
			"a=x-nv-vqos[0].drc.enable:0 \r\n"+
			"a=x-nv-general.enableRecoveryMode:0 \r\n"+
			"a=x-nv-vqos[0].fec.enable:1 \r\n"+
			"a=x-nv-vqos[0].videoQualityScoreUpdateTime:5000 \r\n"+
			"a=x-nv-vqos[0].qosTrafficType:5 \r\n"+
			"a=x-nv-aqos.qosTrafficType:4 \r\n"+
			"a=x-nv-video[0].videoEncoderSlicesPerFrame:1 \r\n"+
			"a=x-nv-clientSupportHevc:0 \r\n"+
			"a=x-nv-vqos[0].bitStreamFormat:0 \r\n"+
			"a=x-nv-video[0].dynamicRangeMode:0 \r\n"+
			"a=x-nv-video[0].maxNumReferenceFrames:1 \r\n"+
			"a=x-nv-video[0].clientRefreshRateX100:%d \r\n"+
			"a=x-nv-audio.surround.numChannels:2 \r\n"+
			"a=x-nv-audio.surround.channelMask:3 \r\n"+
			"a=x-nv-audio.surround.enable:0 \r\n"+
			"a=x-nv-audio.surround.AudioQuality:0 \r\n"+
			"a=x-nv-aqos.packetDuration:5 \r\n"+
			"a=x-nv-video[0].encoderCscMode:0 \r\n",
		nvFeatureFlags,
		mlFeatureFlags,
		width, height, fps,
		adjustedBitrate, adjustedBitrate, adjustedBitrate, adjustedBitrate,
		bitrateKbps,
		fps*100,
	)

	return fmt.Sprintf(
		"v=0\r\n"+
			"o=android 0 %d IN IPv4 %s\r\n"+
			"s=NVIDIA Streaming Client\r\n"+
			"%s"+
			"t=0 0\r\n"+
			"m=video %d  \r\n",
		rtspClientVersion, host, attrs, videoPort,
	)
}
