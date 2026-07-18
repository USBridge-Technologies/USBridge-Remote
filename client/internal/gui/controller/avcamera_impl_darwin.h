#ifndef USBRIDGE_AVCAMERA_IMPL_DARWIN_H
#define USBRIDGE_AVCAMERA_IMPL_DARWIN_H

#include <stdint.h>

int av_camera_open(int reqWidth, int reqHeight, int *outWidth, int *outHeight);
int av_camera_read_frame(uint8_t *buf, int bufLen);
void av_camera_close(void);

#endif
