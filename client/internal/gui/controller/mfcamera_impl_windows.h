#ifndef USBRIDGE_MFCAMERA_IMPL_WINDOWS_H
#define USBRIDGE_MFCAMERA_IMPL_WINDOWS_H

#include <stdint.h>

int mf_camera_open(int reqWidth, int reqHeight, int *outWidth, int *outHeight);
int mf_camera_read_frame(uint8_t *buf, int bufLen);
void mf_camera_close(void);

#endif
