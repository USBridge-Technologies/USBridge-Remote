#ifndef USBRIDGE_V4L2CAMERA_IMPL_LINUX_H
#define USBRIDGE_V4L2CAMERA_IMPL_LINUX_H

#include <stdint.h>

int v4l2_camera_open(int reqWidth, int reqHeight, int *outWidth, int *outHeight);
int v4l2_camera_read_frame(uint8_t *buf, int bufLen);
void v4l2_camera_close(void);

#endif
