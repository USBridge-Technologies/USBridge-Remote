// PyroWave on Metal: the encoder and decoder of vendor/pyrowave ported from Granite/Vulkan to the
// Metal API, for macOS hosts and clients. Same compute shaders (translated by gen_shaders.sh), same
// block layout, quantizer, rate control and packetizing, so the bitstream is the one the Vulkan
// encoders and decoders on Linux and Windows produce and read.
//
// Frames go in and come out as IOSurfaces, biplanar YCbCr wrapped as Metal textures in place -- no
// pixel is copied by the CPU on either side:
// - 4:2:0: 8-bit '420f' / '420v' (NV12), or 16-bit samples -- 10-bit 'xf20' / 'x420' (HDR10 in
//   16-bit containers), 16-bit P016 -- read and written as 16-bit UNORM.
// - 4:4:4: the same with a full-size CbCr plane ('444f' / '444v', 'xf44' / 'x444').
// The wavelet itself has no bit depth: a 16-bit surface just carries finer samples, and the
// stream's colour description is the caller's to stamp (see pwm_encoder_encode).
// Calls are synchronous: they return once the GPU has finished.
//
// An encoder or decoder is used from one thread at a time.
#pragma once

#include <stddef.h>
#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

typedef struct pwm_encoder pwm_encoder;
typedef struct pwm_decoder pwm_decoder;

enum
{
	PWM_OK = 0,
	PWM_ERROR_ARGUMENT = -1,
	// No Metal device, a shader that does not compile, or a GPU that does not run 32-wide SIMD groups.
	PWM_ERROR_UNSUPPORTED = -2,
	PWM_ERROR_GPU = -3,
	// The surface is not 8- or 16-bit biplanar YCbCr of the coder's size and chroma.
	PWM_ERROR_SURFACE = -4,
	// Decoder: a malformed access unit, or one for another size / chroma resolution.
	PWM_ERROR_BITSTREAM = -5,
	// Decoder: the access unit does not hold every block of its frame.
	PWM_ERROR_INCOMPLETE = -6,
	// Encoder: the frame does not fit the output buffer.
	PWM_ERROR_SPACE = -7,
};

// Chroma resolution of an encoder / decoder (the sequence header's chroma_resolution).
enum
{
	PWM_CHROMA_420 = 0,
	PWM_CHROMA_444 = 1,
};

// Why the calling thread's last call failed (empty if it did not).
const char *pwm_last_error(void);

// Whether the coders run on `mtl_device` (an id<MTLDevice>, or NULL for the system default):
// Metal with 32-wide SIMD groups, which every Apple GPU has (an AMD GPU in an Intel Mac runs 64).
// Cheap: compiles one small kernel, not the coders' shaders.
int pwm_supported(void *mtl_device);

// `mtl_device`: an id<MTLDevice> (borrowed), or NULL for the system default device. `chroma`:
// PWM_CHROMA_420 or PWM_CHROMA_444. Width and height even, at most 16384.
int pwm_encoder_create(void *mtl_device, int width, int height, int chroma, pwm_encoder **out);
void pwm_encoder_destroy(pwm_encoder *enc);

// Encodes one frame read straight from `iosurface` (an IOSurfaceRef: biplanar YCbCr of the
// encoder's size and chroma, 8- or 16-bit, either range). `target_size` is the
// rate-controlled budget in bytes. Writes the frame's access unit -- the sequence header and every
// non-empty block record, one dense packet -- to `out` and its length to `out_len`.
//
// The sequence header's colour bits (range, chroma siting, primaries, transfer, matrix) are left
// zero: full range, centred chroma, BT.709. A caller sending something else -- limited range, or
// HDR10's BT.2020 / PQ -- sets them itself.
int pwm_encoder_encode(pwm_encoder *enc, void *iosurface, size_t target_size,
                       uint8_t *out, size_t out_capacity, size_t *out_len);

// GPU time of the last encode, in microseconds.
uint32_t pwm_encoder_last_gpu_us(const pwm_encoder *enc);

int pwm_decoder_create(void *mtl_device, int width, int height, int chroma, pwm_decoder **out);
void pwm_decoder_destroy(pwm_decoder *dec);

// Decodes one access unit (the dense form: sequence header, then block records) into `iosurface`
// (an IOSurfaceRef, Metal-compatible biplanar YCbCr of the decoder's size and chroma, 8- or 16-bit
// -- the caller picks the format from the sequence header's colour bits). Every block of the frame
// must be present (PWM_ERROR_INCOMPLETE otherwise; nothing is written).
int pwm_decoder_decode(pwm_decoder *dec, const uint8_t *au, size_t len, void *iosurface);

uint32_t pwm_decoder_last_gpu_us(const pwm_decoder *dec);

#ifdef __cplusplus
}
#endif
