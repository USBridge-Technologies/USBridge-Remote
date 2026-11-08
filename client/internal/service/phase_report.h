// Phase lock reports (control message 0x5530), shared by every desktop client: how frames
// land against this display's latch, so a host with a source faster than the stream can
// time them to it. A platform only says when a frame was ready and when it was, or
// next can be, on screen; the window, the percentiles and the wire form live in phase_report.c.
#ifndef USBRIDGE_PHASE_REPORT_H
#define USBRIDGE_PHASE_REPORT_H

#include <stdint.h>

// The Client settings switch: applies to the next frame.
void phase_report_set_enabled(int on);
int  phase_report_enabled(void);

// A frame ready at ready_us was shown at present_us (a platform that knows it, any one
// clock). latch_us: how long before the refresh the compositor takes its last frame.
void phase_report_presented(int64_t ready_us, int64_t present_us, int64_t period_us, int64_t latch_us);

// A frame is ready now; the refresh it can make is next_refresh_in_us away (a platform
// that only knows the refresh grid).
void phase_report_ready(int64_t next_refresh_in_us, int64_t period_us, int64_t latch_us, int64_t now_us);

#endif
