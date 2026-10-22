// audio_level.c — the one definition of audio_shed.h's slider level, so every
// platform's audio output (whichever .go file compiles it) reads the same one.
// Written from Go by audio_level_cgo.go.

int g_audio_smooth_level = 0;
