// PyroWave on Metal (see pyrowave_metal.h). A line-by-line port of the host side of
// vendor/pyrowave -- pyrowave_common.cpp (WaveletBuffers), pyrowave_encoder.cpp and
// pyrowave_decoder.cpp at the vendored pin, PYROWAVE_PRECISION 1 (the default build) -- with
// Granite/Vulkan replaced by Metal. The function and member names follow upstream's so the two can
// be compared side by side when the pin moves.
//
// What differs from the Vulkan path:
// - Shaders: pyrowave_metal_shaders.inc (gen_shaders.sh), compiled at runtime from MSL. Apple GPUs
//   run 32-wide SIMD groups in every compute pipeline, which is what the shaders' subgroup-size
//   requirements resolve to (resolve_rate_control's workgroup is pinned to it).
// - Barriers: every dispatch goes into one serial compute encoder, which orders dispatches and
//   makes each one's writes visible to the next -- the role of upstream's cmd.barrier() calls.
// - Input and output planes are IOSurface-backed textures: the encoder samples the capture's Y
//   and CbCr planes in place (Cb and Cr are swizzled views of the one CbCr plane); the decoder
//   writes Y straight into the output surface, and Cb/Cr into two chroma textures that one more
//   dispatch interleaves into the surface's CbCr plane. 8-bit surfaces are R8/RG8 textures,
//   16-bit ones (10-bit HDR in 16-bit containers) R16/RG16 -- both UNORM, so the shaders see the
//   same 0..1 range either way, as on Vulkan.
// - Bitstream and metadata buffers are shared (CPU-visible) memory: read in place after the
//   encode, no staging copy.

#import <Foundation/Foundation.h>
#import <IOSurface/IOSurface.h>
#import <Metal/Metal.h>

#include "pyrowave_metal.h"

#include <algorithm>
#include <cmath>
#include <cstdarg>
#include <cstdio>
#include <cstring>
#include <mutex>
#include <string>
#include <vector>

namespace
{
#include "pyrowave_metal_shaders.inc"

// Hand-written: the decoder's last step, Cb and Cr planes into the output surface's CbCr plane.
const char kPwmShader_cbcr_interleave[] = R"PWMSL(
#include <metal_stdlib>
using namespace metal;
kernel void cbcr_interleave(texture2d<float, access::read> cb [[texture(0)]],
                            texture2d<float, access::read> cr [[texture(1)]],
                            texture2d<float, access::write> cbcr [[texture(2)]],
                            uint2 gid [[thread_position_in_grid]])
{
    if (gid.x >= cbcr.get_width() || gid.y >= cbcr.get_height())
        return;
    cbcr.write(float4(cb.read(gid).r, cr.read(gid).r, 0.0, 1.0), gid);
}
)PWMSL";

// pyrowave_common.hpp
constexpr int DecompositionLevels = 5;
constexpr int Alignment = 1 << DecompositionLevels;
constexpr int MinimumImageSize = 4 << DecompositionLevels;
constexpr int NumComponents = 3;
constexpr int NumFrequencyBandsPerLevel = 4;
constexpr uint32_t SequenceCountMask = 0x7;
constexpr int MaxScaleExp = 4;
// pyrowave_common.cpp: with PRECISION 1 the two finest levels are fp16, the rest fp32.
constexpr int WaveletFP16Levels = 2;
constexpr int Precision = 1;

// pyrowave_encoder.cpp
constexpr int BlockSpaceSubdivision = 16;
constexpr int NumRDOBuckets = 128;
constexpr int RDOBucketOffset = 64;

// gen_shaders.sh puts every push constant block here.
constexpr NSUInteger PushIndex = 30;
// The SIMD width every shader variant was generated for.
constexpr NSUInteger SimdWidth = 32;

struct BitstreamPacket
{
	uint32_t offset_u32;
	uint32_t num_words;
};

struct BitstreamHeader
{
	uint16_t ballot;
	uint16_t payload_words : 12;
	uint16_t sequence : 3;
	uint16_t extended : 1;
	uint32_t quant_code : 8;
	uint32_t block_index : 24;
};
static_assert(sizeof(BitstreamHeader) == 8, "BitstreamHeader is not 8 bytes.");

enum
{
	BITSTREAM_EXTENDED_CODE_START_OF_FRAME = 0,
};

enum
{
	CHROMA_RESOLUTION_420 = 0,
	CHROMA_RESOLUTION_444 = 1,
};

struct BitstreamSequenceHeader
{
	uint32_t width_minus_1 : 14;
	uint32_t height_minus_1 : 14;
	uint32_t sequence : 3;
	uint32_t extended : 1;
	uint32_t total_blocks : 24;
	uint32_t code : 2;
	uint32_t chroma_resolution : 1;
	uint32_t color_primaries : 1;
	uint32_t transfer_function : 1;
	uint32_t ycbcr_transform : 1;
	uint32_t ycbcr_range : 1;
	uint32_t chroma_siting : 1;
};
static_assert(sizeof(BitstreamSequenceHeader) == 8, "BitstreamSequenceHeader is not 8 bytes.");

struct BlockStats
{
	uint32_t num_planes;
	uint16_t stats[15][2];
};
static_assert(sizeof(BlockStats) == 64, "BlockStats is not 64 bytes.");

struct BlockMeta
{
	uint32_t code_word;
	uint32_t offset;
};

struct RDOperation
{
	int32_t quant;
	uint16_t block_offset;
	uint16_t block_saving;
};

struct ivec2
{
	int32_t x, y;
};
struct uvec2
{
	uint32_t x, y;
};
struct vec2
{
	float x, y;
};

thread_local std::string last_error;

int fail(int code, const char *fmt, ...)
{
	char buf[512];
	va_list ap;
	va_start(ap, fmt);
	vsnprintf(buf, sizeof(buf), fmt, ap);
	va_end(ap);
	last_error = buf;
	return code;
}

int align(int value, int alignment)
{
	return (value + alignment - 1) & ~(alignment - 1);
}

uint32_t next_pow2(uint32_t v)
{
	v--;
	v |= v >> 16;
	v |= v >> 8;
	v |= v >> 4;
	v |= v >> 2;
	v |= v >> 1;
	return v + 1;
}

uint32_t floor_log2(uint32_t v)
{
	return 31 - __builtin_clz(v);
}

float decode_quant(uint8_t quant_code)
{
	// Custom FP formulation for numbers in (0, 2) range.
	int e = MaxScaleExp - (quant_code >> 3);
	int m = quant_code & 0x7;
	float inv_quant = (1.0f / (8.0f * 1024.0f * 1024.0f)) * float((8 + m) * (1 << (20 + e)));
	return inv_quant;
}

uint8_t encode_quant(float decoder_q_scale)
{
	uint32_t v;
	memcpy(&v, &decoder_q_scale, sizeof(decoder_q_scale));

	int e = ((v >> 23) & 0xff) - 127 - MaxScaleExp;
	int m = (v >> 20) & 0x7;
	e = -e;
	return uint8_t((e << 3) | m);
}

int compute_block_count_per_subdivision(int num_blocks)
{
	int per_subdivision = align(num_blocks, BlockSpaceSubdivision) / BlockSpaceSubdivision;
	per_subdivision = int(next_pow2(uint32_t(per_subdivision)));
	return per_subdivision;
}

// ── shaders ───────────────────────────────────────────────────────────────────

struct Pipelines
{
	id<MTLComputePipelineState> dwt[2]; // [DCShift]
	id<MTLComputePipelineState> wavelet_quant;
	id<MTLComputePipelineState> analyze_rate_control;
	id<MTLComputePipelineState> analyze_rate_control_finalize;
	id<MTLComputePipelineState> resolve_rate_control;
	id<MTLComputePipelineState> block_packing;
	id<MTLComputePipelineState> wavelet_dequant;
	id<MTLComputePipelineState> idwt[2]; // [DCShift]
	id<MTLComputePipelineState> cbcr_interleave;
};

id<MTLComputePipelineState> make_pipeline(id<MTLDevice> device, const char *source, const char *name,
                                          MTLFunctionConstantValues *constants, NSUInteger threads)
{
	MTLCompileOptions *options = [MTLCompileOptions new];
	// gen_shaders.sh generates MSL 2.3 (macOS 11, the first release on Apple Silicon).
	if (@available(macOS 11.0, *))
		options.languageVersion = MTLLanguageVersion2_3;
	else
	{
		fail(PWM_ERROR_UNSUPPORTED, "needs macOS 11 (Metal Shading Language 2.3)");
		return nil;
	}
	// IEEE float math, as the Vulkan drivers run these shaders.
	if (@available(macOS 15.0, *))
		options.mathMode = MTLMathModeSafe;
	else
	{
#pragma clang diagnostic push
#pragma clang diagnostic ignored "-Wdeprecated-declarations"
		options.fastMathEnabled = NO;
#pragma clang diagnostic pop
	}
	NSError *error = nil;
	id<MTLLibrary> library = [device newLibraryWithSource:@(source) options:options error:&error];
	if (!library)
	{
		fail(PWM_ERROR_UNSUPPORTED, "compiling %s: %s", name, error.localizedDescription.UTF8String);
		return nil;
	}
	// A function with function constants (even ones left at their defaults) has to be specialized.
	id<MTLFunction> function = [library newFunctionWithName:@(name)
	                                         constantValues:(constants ? constants : [MTLFunctionConstantValues new])
	                                                  error:&error];
	if (!function)
	{
		fail(PWM_ERROR_UNSUPPORTED, "no function %s: %s", name, error ? error.localizedDescription.UTF8String : "");
		return nil;
	}
	id<MTLComputePipelineState> pso = [device newComputePipelineStateWithFunction:function error:&error];
	if (!pso)
	{
		fail(PWM_ERROR_UNSUPPORTED, "pipeline %s: %s", name, error.localizedDescription.UTF8String);
		return nil;
	}
	if (pso.threadExecutionWidth != SimdWidth || pso.maxTotalThreadsPerThreadgroup < threads)
	{
		fail(PWM_ERROR_UNSUPPORTED, "pipeline %s: SIMD width %u and at most %u threads, needs %u and %u", name,
		     unsigned(pso.threadExecutionWidth), unsigned(pso.maxTotalThreadsPerThreadgroup),
		     unsigned(SimdWidth), unsigned(threads));
		return nil;
	}
	return pso;
}

MTLFunctionConstantValues *bool_constant(NSUInteger index, bool value)
{
	MTLFunctionConstantValues *values = [MTLFunctionConstantValues new];
	[values setConstantValue:&value type:MTLDataTypeBool atIndex:index];
	return values;
}

bool make_encoder_pipelines(id<MTLDevice> device, Pipelines &p)
{
	uint32_t subgroup_size = SimdWidth;
	MTLFunctionConstantValues *resolve_constants = [MTLFunctionConstantValues new];
	[resolve_constants setConstantValue:&subgroup_size type:MTLDataTypeUInt atIndex:0];

	return (p.dwt[0] = make_pipeline(device, kPwmShader_dwt, "dwt", bool_constant(0, false), 64)) &&
	       (p.dwt[1] = make_pipeline(device, kPwmShader_dwt, "dwt", bool_constant(0, true), 64)) &&
	       (p.wavelet_quant = make_pipeline(device, kPwmShader_wavelet_quant, "wavelet_quant", nil, 128)) &&
	       (p.analyze_rate_control = make_pipeline(device, kPwmShader_analyze_rate_control, "analyze_rate_control", nil, 64)) &&
	       (p.analyze_rate_control_finalize = make_pipeline(device, kPwmShader_analyze_rate_control_finalize,
	                                                        "analyze_rate_control_finalize", nil, 512)) &&
	       (p.resolve_rate_control = make_pipeline(device, kPwmShader_resolve_rate_control, "resolve_rate_control",
	                                               resolve_constants, SimdWidth)) &&
	       (p.block_packing = make_pipeline(device, kPwmShader_block_packing, "block_packing", nil, 64));
}

bool make_decoder_pipelines(id<MTLDevice> device, Pipelines &p)
{
	return (p.wavelet_dequant = make_pipeline(device, kPwmShader_wavelet_dequant, "wavelet_dequant", nil, 128)) &&
	       (p.idwt[0] = make_pipeline(device, kPwmShader_idwt, "idwt", bool_constant(0, false), 64)) &&
	       (p.idwt[1] = make_pipeline(device, kPwmShader_idwt, "idwt", bool_constant(0, true), 64)) &&
	       (p.cbcr_interleave = make_pipeline(device, kPwmShader_cbcr_interleave, "cbcr_interleave", nil, 64));
}

// Compiling takes up to a second the first time on a machine (Metal keeps the result in its own
// on-disk cache after that); done once per process and device, encoder and decoder halves apart.
const Pipelines *get_pipelines(id<MTLDevice> device, bool for_encoder)
{
	static std::mutex lock;
	static id<MTLDevice> cached_device;
	static Pipelines cached;
	static bool encoder_ready = false, decoder_ready = false;

	std::lock_guard<std::mutex> holder(lock);
	if (cached_device != device)
	{
		cached = {};
		cached_device = device;
		encoder_ready = decoder_ready = false;
	}

	bool &ready = for_encoder ? encoder_ready : decoder_ready;
	if (!ready)
	{
		Pipelines p = cached;
		if (!(for_encoder ? make_encoder_pipelines(device, p) : make_decoder_pipelines(device, p)))
			return nullptr;
		cached = p;
		ready = true;
	}
	return &cached;
}

void set_push(id<MTLComputeCommandEncoder> enc, const void *data, size_t size)
{
	// MSL rounds each block up to its largest member's alignment; hand it whole 16-byte rows.
	uint8_t padded[128] = {};
	memcpy(padded, data, size);
	[enc setBytes:padded length:(size + 15) & ~size_t(15) atIndex:PushIndex];
}

void dispatch(id<MTLComputeCommandEncoder> enc, NSUInteger x, NSUInteger y, NSUInteger threads)
{
	[enc dispatchThreadgroups:MTLSizeMake(x, y, 1) threadsPerThreadgroup:MTLSizeMake(threads, 1, 1)];
}

id<MTLDevice> resolve_device(void *mtl_device)
{
	if (mtl_device)
		return (__bridge id<MTLDevice>)mtl_device;
	return MTLCreateSystemDefaultDevice();
}

// ── WaveletBuffers (pyrowave_common.cpp) ──────────────────────────────────────

struct BlockInfo
{
	int block_offset_8x8;
	int block_stride_8x8;
	int block_offset_32x32;
	int block_stride_32x32;
	int block_count_32x32;
};

struct BlockMapping
{
	int block_offset_8x8;
	int block_stride_8x8;
	int block_width_8x8;
	int block_height_8x8;
};

struct WaveletBuffers
{
	id<MTLDevice> device;
	const Pipelines *pipelines = nullptr;
	id<MTLCommandQueue> queue;
	id<MTLTexture> wavelet_img_high_res;
	id<MTLTexture> wavelet_img_low_res;
	id<MTLSamplerState> mirror_repeat_sampler;
	id<MTLSamplerState> border_sampler;
	id<MTLTexture> component_layer_views[NumComponents][DecompositionLevels];
	id<MTLTexture> component_ll_views[NumComponents][DecompositionLevels];

	BlockInfo block_meta[NumComponents][DecompositionLevels][4] = {};
	std::vector<BlockMapping> block_32x32_to_8x8_mapping;
	int block_count_8x8 = 0;
	int block_count_32x32 = 0;

	int width = 0;
	int height = 0;
	bool chroma444 = false;
	int aligned_width = 0;
	int aligned_height = 0;
	uint32_t last_gpu_us = 0;

	// Image::get_width(level) of wavelet_img_high_res: the level size, also past its own mips.
	int level_width(int level) const
	{
		return std::max(1, (aligned_width / 2) >> level);
	}
	int level_height(int level) const
	{
		return std::max(1, (aligned_height / 2) >> level);
	}

	// 4:2:0 has no level-0 chroma bands: the half-size chroma planes enter at level 1.
	bool skip_band_level(int level, int component) const
	{
		return level == 0 && component != 0 && !chroma444;
	}

	int init(void *mtl_device, int width_, int height_, int chroma, bool for_encoder)
	{
		if (chroma != PWM_CHROMA_420 && chroma != PWM_CHROMA_444)
			return fail(PWM_ERROR_ARGUMENT, "chroma %d", chroma);
		if (width_ <= 0 || height_ <= 0 || (width_ & 1) || (height_ & 1) || width_ > 16384 || height_ > 16384)
			return fail(PWM_ERROR_ARGUMENT, "%dx%d is not an even size", width_, height_);
		chroma444 = chroma == PWM_CHROMA_444;
		device = resolve_device(mtl_device);
		if (!device)
			return fail(PWM_ERROR_UNSUPPORTED, "no Metal device");
		pipelines = get_pipelines(device, for_encoder);
		if (!pipelines)
			return PWM_ERROR_UNSUPPORTED;
		queue = [device newCommandQueue];
		if (!queue)
			return fail(PWM_ERROR_GPU, "newCommandQueue failed");

		width = width_;
		height = height_;
		aligned_width = std::max(align(width, Alignment), MinimumImageSize);
		aligned_height = std::max(align(height, Alignment), MinimumImageSize);

		init_samplers();
		if (!allocate_images())
			return fail(PWM_ERROR_GPU, "allocating the wavelet images failed");
		init_block_meta();
		return PWM_OK;
	}

	void init_samplers()
	{
		MTLSamplerDescriptor *samp = [MTLSamplerDescriptor new];
		samp.minFilter = MTLSamplerMinMagFilterNearest;
		samp.magFilter = MTLSamplerMinMagFilterNearest;
		samp.mipFilter = MTLSamplerMipFilterNearest;
		samp.normalizedCoordinates = YES;
		samp.sAddressMode = MTLSamplerAddressModeMirrorRepeat;
		samp.tAddressMode = MTLSamplerAddressModeMirrorRepeat;
		samp.rAddressMode = MTLSamplerAddressModeMirrorRepeat;
		mirror_repeat_sampler = [device newSamplerStateWithDescriptor:samp];

		samp.sAddressMode = MTLSamplerAddressModeClampToBorderColor;
		samp.tAddressMode = MTLSamplerAddressModeClampToBorderColor;
		samp.rAddressMode = MTLSamplerAddressModeClampToBorderColor;
		samp.borderColor = MTLSamplerBorderColorTransparentBlack;
		border_sampler = [device newSamplerStateWithDescriptor:samp];
	}

	bool allocate_images()
	{
		MTLTextureDescriptor *info = [MTLTextureDescriptor new];
		info.textureType = MTLTextureType2DArray;
		info.pixelFormat = MTLPixelFormatR16Float;
		info.width = NSUInteger(aligned_width / 2);
		info.height = NSUInteger(aligned_height / 2);
		info.arrayLength = NumFrequencyBandsPerLevel * NumComponents;
		info.mipmapLevelCount = WaveletFP16Levels;
		info.usage = MTLTextureUsageShaderRead | MTLTextureUsageShaderWrite | MTLTextureUsagePixelFormatView;
		info.storageMode = MTLStorageModePrivate;
		wavelet_img_high_res = [device newTextureWithDescriptor:info];

		// For the lowest level bands, we want to maintain precision as much as possible and bandwidth here is trivial.
		info.pixelFormat = MTLPixelFormatR32Float;
		info.mipmapLevelCount = DecompositionLevels - WaveletFP16Levels;
		info.width = NSUInteger(aligned_width / 2) >> WaveletFP16Levels;
		info.height = NSUInteger(aligned_height / 2) >> WaveletFP16Levels;
		wavelet_img_low_res = [device newTextureWithDescriptor:info];
		if (!wavelet_img_high_res || !wavelet_img_low_res)
			return false;

		for (int level = 0; level < DecompositionLevels; level++)
		{
			id<MTLTexture> image = level < WaveletFP16Levels ? wavelet_img_high_res : wavelet_img_low_res;
			NSUInteger base_level = NSUInteger(level < WaveletFP16Levels ? level : level - WaveletFP16Levels);

			for (int component = 0; component < NumComponents; component++)
			{
				NSUInteger base_layer = NSUInteger(4 * component);
				component_layer_views[component][level] =
				    [image newTextureViewWithPixelFormat:image.pixelFormat
				                             textureType:MTLTextureType2DArray
				                                  levels:NSMakeRange(base_level, 1)
				                                  slices:NSMakeRange(base_layer, 4)];
				component_ll_views[component][level] =
				    [image newTextureViewWithPixelFormat:image.pixelFormat
				                             textureType:MTLTextureType2D
				                                  levels:NSMakeRange(base_level, 1)
				                                  slices:NSMakeRange(base_layer, 1)];
				if (!component_layer_views[component][level] || !component_ll_views[component][level])
					return false;
			}
		}
		return true;
	}

	void accumulate_block_mapping(int blocks_x_8x8, int blocks_y_8x8)
	{
		int blocks_x_32x32 = (blocks_x_8x8 + 3) / 4;
		int blocks_y_32x32 = (blocks_y_8x8 + 3) / 4;

		for (int y = 0; y < blocks_y_32x32; y++)
		{
			for (int x = 0; x < blocks_x_32x32; x++)
			{
				BlockMapping mapping = {};
				mapping.block_offset_8x8 = block_count_8x8 + 4 * y * blocks_x_8x8 + 4 * x;
				mapping.block_stride_8x8 = blocks_x_8x8;
				mapping.block_width_8x8 = std::min<int>(4, blocks_x_8x8 - 4 * x);
				mapping.block_height_8x8 = std::min<int>(4, blocks_y_8x8 - 4 * y);
				block_32x32_to_8x8_mapping.push_back(mapping);
				block_count_32x32++;
			}
		}

		block_count_8x8 += blocks_x_8x8 * blocks_y_8x8;
	}

	void init_block_meta()
	{
		for (int level = DecompositionLevels - 1; level >= 0; level--)
		{
			for (int component = 0; component < NumComponents; component++)
			{
				// Ignore top-level CbCr when doing 420 subsampling.
				if (skip_band_level(level, component))
					continue;

				for (int band = (level == DecompositionLevels - 1 ? 0 : 1); band < 4; band++)
				{
					int level_width_ = level_width(level);
					int level_height_ = level_height(level);

					int blocks_x_8x8 = (level_width_ + 7) / 8;
					int blocks_y_8x8 = (level_height_ + 7) / 8;
					int blocks_x_32x32 = (level_width_ + 31) / 32;
					int blocks_y_32x32 = (level_height_ + 31) / 32;

					block_meta[component][level][band] = {
						block_count_8x8, blocks_x_8x8,
						block_count_32x32, blocks_x_32x32,
						blocks_x_32x32 * blocks_y_32x32
					};

					accumulate_block_mapping(blocks_x_8x8, blocks_y_8x8);
				}
			}
		}
	}

	id<MTLBuffer> device_buffer(size_t size)
	{
		return [device newBufferWithLength:std::max<size_t>(size, 16) options:MTLResourceStorageModePrivate];
	}

	id<MTLBuffer> shared_buffer(size_t size)
	{
		return [device newBufferWithLength:std::max<size_t>(size, 16)
		                           options:MTLResourceStorageModeShared | MTLResourceCPUCacheModeDefaultCache];
	}

	// Commits `cmd` and waits for it. PWM_OK, or why the GPU did not finish it.
	int run(id<MTLCommandBuffer> cmd)
	{
		[cmd commit];
		[cmd waitUntilCompleted];
		if (cmd.status != MTLCommandBufferStatusCompleted)
			return fail(PWM_ERROR_GPU, "command buffer failed: %s",
			            cmd.error ? cmd.error.localizedDescription.UTF8String : "unknown error");
		last_gpu_us = uint32_t(std::max(0.0, cmd.GPUEndTime - cmd.GPUStartTime) * 1e6);
		return PWM_OK;
	}
};

// ── IOSurface planes as textures ──────────────────────────────────────────────

// One biplanar YCbCr IOSurface wrapped as textures, kept while the surface is in use: capture and
// display pools hand the same few surfaces round, so each is wrapped once.
struct SurfaceTextures
{
	IOSurfaceRef surface = nullptr; // retained
	// 2 for 16-bit samples (10-bit formats keep theirs in 16-bit containers), else 1.
	size_t bytes_per_sample = 1;
	id<MTLTexture> y;
	id<MTLTexture> cbcr;
	// Encoder: Cb and Cr as single-channel reads of the CbCr plane (red, green).
	id<MTLTexture> cb;
	id<MTLTexture> cr;
};

struct SurfaceCache
{
	static constexpr size_t MaxSurfaces = 16;
	std::vector<SurfaceTextures> entries;

	~SurfaceCache()
	{
		clear();
	}

	void clear()
	{
		for (auto &e : entries)
			CFRelease(e.surface);
		entries.clear();
	}

	// `writable`: the decoder's output; otherwise the encoder's input. The surface is 8- or 16-bit
	// biplanar YCbCr of the coder's size, its CbCr plane half size (4:2:0) or full size (4:4:4).
	const SurfaceTextures *get(id<MTLDevice> device, IOSurfaceRef surface, int width, int height, bool chroma444,
	                           bool writable)
	{
		for (auto &e : entries)
			if (e.surface == surface)
				return &e;

		const int chroma_width = chroma444 ? width : width / 2;
		const int chroma_height = chroma444 ? height : height / 2;
		const size_t bps = IOSurfaceGetBytesPerElementOfPlane(surface, 0);
		if (IOSurfaceGetPlaneCount(surface) != 2 || (bps != 1 && bps != 2) ||
		    int(IOSurfaceGetWidthOfPlane(surface, 0)) != width || int(IOSurfaceGetHeightOfPlane(surface, 0)) != height ||
		    int(IOSurfaceGetWidthOfPlane(surface, 1)) != chroma_width ||
		    int(IOSurfaceGetHeightOfPlane(surface, 1)) != chroma_height ||
		    IOSurfaceGetBytesPerElementOfPlane(surface, 1) != 2 * bps)
		{
			fail(PWM_ERROR_SURFACE, "surface is %zux%zu with %zu planes of %zu bytes per sample, not biplanar %s %dx%d",
			     IOSurfaceGetWidth(surface), IOSurfaceGetHeight(surface), IOSurfaceGetPlaneCount(surface), bps,
			     chroma444 ? "4:4:4" : "4:2:0", width, height);
			return nullptr;
		}

		MTLTextureUsage usage = writable ? (MTLTextureUsageShaderRead | MTLTextureUsageShaderWrite) : MTLTextureUsageShaderRead;
		MTLTextureDescriptor *desc = [MTLTextureDescriptor texture2DDescriptorWithPixelFormat:(bps == 2 ? MTLPixelFormatR16Unorm : MTLPixelFormatR8Unorm)
		                                                                                width:NSUInteger(width)
		                                                                               height:NSUInteger(height)
		                                                                            mipmapped:NO];
		desc.usage = usage;
		desc.storageMode = MTLStorageModeShared;
		SurfaceTextures e;
		e.bytes_per_sample = bps;
		e.y = [device newTextureWithDescriptor:desc iosurface:surface plane:0];

		desc.pixelFormat = bps == 2 ? MTLPixelFormatRG16Unorm : MTLPixelFormatRG8Unorm;
		desc.width = NSUInteger(chroma_width);
		desc.height = NSUInteger(chroma_height);
		e.cbcr = [device newTextureWithDescriptor:desc iosurface:surface plane:1];
		if (!writable)
		{
			desc.swizzle = MTLTextureSwizzleChannelsMake(MTLTextureSwizzleRed, MTLTextureSwizzleRed,
			                                             MTLTextureSwizzleRed, MTLTextureSwizzleRed);
			e.cb = [device newTextureWithDescriptor:desc iosurface:surface plane:1];
			desc.swizzle = MTLTextureSwizzleChannelsMake(MTLTextureSwizzleGreen, MTLTextureSwizzleGreen,
			                                             MTLTextureSwizzleGreen, MTLTextureSwizzleGreen);
			e.cr = [device newTextureWithDescriptor:desc iosurface:surface plane:1];
		}
		if (!e.y || !e.cbcr || (!writable && (!e.cb || !e.cr)))
		{
			fail(PWM_ERROR_SURFACE, "wrapping the surface's planes as Metal textures failed");
			return nullptr;
		}

		if (entries.size() >= MaxSurfaces)
			clear(); // a size change or a new pool left stale surfaces behind
		CFRetain(surface);
		e.surface = surface;
		entries.push_back(e);
		return &entries.back();
	}
};

// ── Encoder (pyrowave_encoder.cpp) ────────────────────────────────────────────

struct QuantizerPushData
{
	ivec2 resolution;
	ivec2 resolution_8x8_blocks;
	vec2 inv_resolution;
	float input_layer;
	float quant_resolution;
	int32_t block_offset;
	int32_t block_stride;
	float rdo_distortion_scale;
};

struct BlockPackingPushData
{
	ivec2 resolution;
	ivec2 resolution_32x32_blocks;
	ivec2 resolution_8x8_blocks;
	uint32_t quant_resolution_code;
	uint32_t sequence_count;
	uint32_t block_offset_32x32;
	uint32_t block_stride_32x32;
	uint32_t block_offset_8x8;
	uint32_t block_stride_8x8;
};

struct AnalyzeRateControlPushData
{
	ivec2 resolution;
	ivec2 resolution_8x8_blocks;
	int32_t block_offset_8x8;
	int32_t block_stride_8x8;
	int32_t block_offset_32x32;
	int32_t block_stride_32x32;
	uint32_t total_wg_count;
	uint32_t num_blocks_aligned;
	uint32_t block_index_shamt;
};

struct EncoderImpl : WaveletBuffers
{
	id<MTLBuffer> bucket_buffer, meta_buffer, block_stat_buffer, payload_data, quant_buffer;
	// The frame's BitstreamPacket array and block records, read by the CPU after the encode.
	id<MTLBuffer> out_meta, out_bitstream;
	SurfaceCache surfaces;
	uint32_t sequence_count = 0;

	float get_noise_power_normalized_quant_resolution(int level, int component, int band) const
	{
		// The initial quantization resolution aims for a flat spectrum with noise power normalization.
		// The low-pass gain for CDF 9/7 is 6 dB (1 bit). Every decomposition level subtracts 6 dB.
		int bits = Precision >= 1 ? 8 : 6;

		if (band == 0)
			bits += 2;
		else if (band < 3)
			bits += 1;

		bits += level;

		// Chroma starts at level 1, subtract one bit.
		if (component != 0)
			bits--;

		return float(1 << bits);
	}

	float get_quant_resolution(int level, int component, int band) const
	{
		// FP16 range is limited, and this is more than a good enough initial estimate.
		return std::min<float>(Precision >= 1 ? 4096.0f : 512.0f,
		                       get_noise_power_normalized_quant_resolution(level, component, band));
	}

	float get_quant_rdo_distortion_scale(int level, int component, int band) const
	{
		float horiz_midpoint = (band & 1) ? 0.75f : 0.25f;
		float vert_midpoint = (band & 2) ? 0.75f : 0.25f;

		// Normal PC monitors.
		constexpr float dpi = 96.0f;
		// Compromise between couch gaming and desktop.
		constexpr float viewing_distance = 1.0f;
		constexpr float cpd_nyquist = 0.34f * viewing_distance * dpi;

		float cpd = std::sqrt(horiz_midpoint * horiz_midpoint + vert_midpoint * vert_midpoint) *
		            cpd_nyquist * std::exp2(-float(level));

		// Don't allow a situation where we're quantizing LL band hard.
		cpd = std::max(cpd, 8.0f);

		float csf = 2.6f * (0.0192f + 0.114f * cpd) * std::exp(-std::pow(0.114f * cpd, 1.1f));

		// Heavily discount chroma quality.
		if (component != 0 && level != DecompositionLevels - 1)
		{
			// Consider chroma a little more important if we're not subsampling.
			if (!chroma444)
				csf *= 0.6f;
		}

		// Due to filtering, distortion in lower bands will result in more noise power.
		// By scaling the distortion by this factor, we ensure uniform results.
		float resolution = get_noise_power_normalized_quant_resolution(level, component, band);
		float weighted_resolution = csf * resolution;

		// The distortion is scaled in terms of power, not amplitude.
		return weighted_resolution * weighted_resolution;
	}

	bool init_encoder_buffers()
	{
		block_stat_buffer = device_buffer(size_t(block_count_8x8) * sizeof(BlockStats));
		meta_buffer = device_buffer(size_t(block_count_8x8) * sizeof(BlockMeta));
		// Worst case estimate; 4:4:4 carries twice the samples (patches/0001).
		payload_data = device_buffer(size_t(aligned_width) * size_t(aligned_height) * (chroma444 ? 4 : 2));
		quant_buffer = device_buffer(size_t(block_count_32x32) * sizeof(uint32_t));

		size_t bucket_size = RDOBucketOffset;
		bucket_size += NumRDOBuckets * BlockSpaceSubdivision * sizeof(uint32_t);
		bucket_size += size_t(NumRDOBuckets) * size_t(compute_block_count_per_subdivision(block_count_32x32)) *
		               BlockSpaceSubdivision * sizeof(RDOperation);
		bucket_buffer = device_buffer(bucket_size);

		out_meta = shared_buffer(size_t(block_count_32x32) * sizeof(BitstreamPacket));
		return block_stat_buffer && meta_buffer && payload_data && quant_buffer && bucket_buffer && out_meta;
	}

	void dwt(id<MTLComputeCommandEncoder> cmd, const SurfaceTextures &planes)
	{
		struct Push
		{
			uvec2 resolution;
			vec2 inv_resolution;
			uvec2 aligned_resolution;
		} push = {};

		[cmd setSamplerState:mirror_repeat_sampler atIndex:0];

		for (int output_level = 0; output_level < DecompositionLevels; output_level++)
		{
			if (output_level > 0)
			{
				push.resolution = { uint32_t(level_width(output_level - 1)), uint32_t(level_height(output_level - 1)) };
				push.aligned_resolution = push.resolution;
			}
			else
			{
				push.resolution = { uint32_t(width), uint32_t(height) };
				push.aligned_resolution = { uint32_t(aligned_width), uint32_t(aligned_height) };
			}

			push.inv_resolution = { 1.0f / float(push.resolution.x), 1.0f / float(push.resolution.y) };
			set_push(cmd, &push, sizeof(push));

			if (output_level == 0)
			{
				[cmd setComputePipelineState:pipelines->dwt[1]];
				for (int c = 0; c < (chroma444 ? NumComponents : 1); c++)
				{
					[cmd setTexture:(c == 0 ? planes.y : c == 1 ? planes.cb : planes.cr) atIndex:0];
					[cmd setTexture:component_layer_views[c][output_level] atIndex:1];
					dispatch(cmd, (push.aligned_resolution.x + 31) / 32, (push.aligned_resolution.y + 31) / 32, 64);
				}
			}
			else
			{
				for (int c = 0; c < NumComponents; c++)
				{
					if (!chroma444 && c != 0 && output_level == 1)
					{
						push.resolution = { uint32_t(width / 2), uint32_t(height / 2) };
						push.aligned_resolution = { uint32_t(aligned_width >> output_level), uint32_t(aligned_height >> output_level) };
						push.inv_resolution = { 1.0f / float(push.resolution.x), 1.0f / float(push.resolution.y) };
						set_push(cmd, &push, sizeof(push));
						[cmd setTexture:(c == 1 ? planes.cb : planes.cr) atIndex:0];
						[cmd setComputePipelineState:pipelines->dwt[1]];
					}
					else
					{
						[cmd setTexture:component_ll_views[c][output_level - 1] atIndex:0];
						[cmd setComputePipelineState:pipelines->dwt[0]];
					}

					[cmd setTexture:component_layer_views[c][output_level] atIndex:1];
					dispatch(cmd, (push.aligned_resolution.x + 31) / 32, (push.aligned_resolution.y + 31) / 32, 64);
				}
			}
		}
	}

	void quant(id<MTLComputeCommandEncoder> cmd)
	{
		[cmd setComputePipelineState:pipelines->wavelet_quant];
		[cmd setSamplerState:border_sampler atIndex:0];
		[cmd setBuffer:meta_buffer offset:0 atIndex:1];
		[cmd setBuffer:block_stat_buffer offset:0 atIndex:2];
		[cmd setBuffer:payload_data offset:0 atIndex:3];

		for (int level = 0; level < DecompositionLevels; level++)
		{
			for (int component = 0; component < NumComponents; component++)
			{
				// Ignore top-level CbCr when doing 420 subsampling.
				if (skip_band_level(level, component))
					continue;

				[cmd setTexture:component_layer_views[component][level] atIndex:0];

				for (int band = (level == DecompositionLevels - 1 ? 0 : 1); band < 4; band++)
				{
					float quant_res = get_quant_resolution(level, component, band);

					QuantizerPushData push = {};
					push.resolution = { level_width(level), level_height(level) };
					push.resolution_8x8_blocks = { (push.resolution.x + 7) / 8, (push.resolution.y + 7) / 8 };
					push.inv_resolution = { 1.0f / float(push.resolution.x), 1.0f / float(push.resolution.y) };
					push.input_layer = float(band);
					push.quant_resolution = 1.0f / decode_quant(encode_quant(1.0f / quant_res));
					push.rdo_distortion_scale = get_quant_rdo_distortion_scale(level, component, band) * (1.0f / 256.0f);
					push.block_offset = block_meta[component][level][band].block_offset_8x8;
					push.block_stride = block_meta[component][level][band].block_stride_8x8;
					set_push(cmd, &push, sizeof(push));

					int blocks_x = (push.resolution.x + 31) / 32;
					int blocks_y = (push.resolution.y + 31) / 32;
					dispatch(cmd, NSUInteger(blocks_x), NSUInteger(blocks_y), 128);
				}
			}
		}
	}

	void analyze_rdo(id<MTLComputeCommandEncoder> cmd)
	{
		[cmd setComputePipelineState:pipelines->analyze_rate_control];
		[cmd setBuffer:bucket_buffer offset:0 atIndex:0];
		[cmd setBuffer:block_stat_buffer offset:0 atIndex:1];

		int per_subdivision = compute_block_count_per_subdivision(block_count_32x32);

		for (int level = 0; level < DecompositionLevels; level++)
		{
			for (int component = 0; component < NumComponents; component++)
			{
				// Ignore top-level CbCr when doing 420 subsampling.
				if (skip_band_level(level, component))
					continue;

				for (int band = (level == DecompositionLevels - 1 ? 0 : 1); band < 4; band++)
				{
					int level_width_ = level_width(level);
					int level_height_ = level_height(level);

					AnalyzeRateControlPushData push = {};
					push.resolution = { level_width_, level_height_ };
					push.resolution_8x8_blocks = { (level_width_ + 7) / 8, (level_height_ + 7) / 8 };
					push.block_offset_8x8 = block_meta[component][level][band].block_offset_8x8;
					push.block_stride_8x8 = block_meta[component][level][band].block_stride_8x8;
					push.block_offset_32x32 = block_meta[component][level][band].block_offset_32x32;
					push.block_stride_32x32 = block_meta[component][level][band].block_stride_32x32;
					push.total_wg_count = uint32_t(block_count_32x32);
					push.num_blocks_aligned = uint32_t(per_subdivision * BlockSpaceSubdivision);
					push.block_index_shamt = floor_log2(uint32_t(per_subdivision));
					set_push(cmd, &push, sizeof(push));

					dispatch(cmd, NSUInteger((level_width_ + 31) / 32), NSUInteger((level_height_ + 31) / 32), 64);
				}
			}
		}

		[cmd setComputePipelineState:pipelines->analyze_rate_control_finalize];
		dispatch(cmd, 1, 1, 512);
	}

	void resolve_rdo(id<MTLComputeCommandEncoder> cmd, size_t target_payload_size)
	{
		if (target_payload_size >= sizeof(BitstreamSequenceHeader))
			target_payload_size -= sizeof(BitstreamSequenceHeader);

		struct
		{
			uint32_t target_payload_size;
			uint32_t num_blocks_per_subdivision;
		} push = {};

		push.target_payload_size = uint32_t(target_payload_size / sizeof(uint32_t));
		push.num_blocks_per_subdivision = uint32_t(compute_block_count_per_subdivision(block_count_32x32));

		[cmd setComputePipelineState:pipelines->resolve_rate_control];
		set_push(cmd, &push, sizeof(push));
		[cmd setBuffer:bucket_buffer offset:0 atIndex:0];
		[cmd setBuffer:quant_buffer offset:0 atIndex:1];
		dispatch(cmd, NumRDOBuckets * BlockSpaceSubdivision, 1, SimdWidth);
	}

	void block_packing(id<MTLComputeCommandEncoder> cmd)
	{
		[cmd setComputePipelineState:pipelines->block_packing];
		[cmd setBuffer:out_bitstream offset:0 atIndex:0];
		[cmd setBuffer:out_meta offset:0 atIndex:1];
		[cmd setBuffer:meta_buffer offset:0 atIndex:2];
		[cmd setBuffer:payload_data offset:0 atIndex:3];
		[cmd setBuffer:block_stat_buffer offset:0 atIndex:4];
		[cmd setBuffer:quant_buffer offset:0 atIndex:5];

		for (int level = 0; level < DecompositionLevels; level++)
		{
			int level_width_ = level_width(level);
			int level_height_ = level_height(level);

			for (int component = 0; component < NumComponents; component++)
			{
				// Ignore top-level CbCr when doing 420 subsampling.
				if (skip_band_level(level, component))
					continue;

				for (int band = (level == DecompositionLevels - 1 ? 0 : 1); band < 4; band++)
				{
					BlockPackingPushData packing_push = {};
					packing_push.resolution = { level_width_, level_height_ };
					packing_push.resolution_32x32_blocks = { (level_width_ + 31) / 32, (level_height_ + 31) / 32 };
					packing_push.resolution_8x8_blocks = { (level_width_ + 7) / 8, (level_height_ + 7) / 8 };

					auto quant_res = get_quant_resolution(level, component, band);
					packing_push.quant_resolution_code = encode_quant(1.0f / quant_res);
					packing_push.sequence_count = sequence_count;

					auto &meta = block_meta[component][level][band];
					packing_push.block_offset_32x32 = uint32_t(meta.block_offset_32x32);
					packing_push.block_stride_32x32 = uint32_t(meta.block_stride_32x32);
					packing_push.block_offset_8x8 = uint32_t(meta.block_offset_8x8);
					packing_push.block_stride_8x8 = uint32_t(meta.block_stride_8x8);
					set_push(cmd, &packing_push, sizeof(packing_push));

					dispatch(cmd, NSUInteger((packing_push.resolution_32x32_blocks.x + 1) / 2),
					         NSUInteger((packing_push.resolution_32x32_blocks.y + 1) / 2), 64);
				}
			}
		}
	}

	int encode(IOSurfaceRef surface, size_t maximum_bitstream_size)
	{
		auto target_bitstream_size = maximum_bitstream_size & ~size_t(3u);
		if (target_bitstream_size > UINT32_MAX || target_bitstream_size == 0)
			return fail(PWM_ERROR_ARGUMENT, "bitstream budget %zu", maximum_bitstream_size);

		const SurfaceTextures *planes = surfaces.get(device, surface, width, height, chroma444, false);
		if (!planes)
			return PWM_ERROR_SURFACE;

		const size_t meta_size = size_t(block_count_32x32) * sizeof(BitstreamPacket);
		const size_t bitstream_size = target_bitstream_size + meta_size;
		if (!out_bitstream || out_bitstream.length < bitstream_size)
		{
			out_bitstream = shared_buffer(bitstream_size);
			if (!out_bitstream)
				return fail(PWM_ERROR_GPU, "allocating a %zu-byte bitstream buffer failed", bitstream_size);
		}

		sequence_count = (sequence_count + 1) & SequenceCountMask;

		id<MTLCommandBuffer> cmd_buf = [queue commandBuffer];
		id<MTLBlitCommandEncoder> blit = [cmd_buf blitCommandEncoder];
		[blit fillBuffer:payload_data range:NSMakeRange(0, 2 * sizeof(uint32_t)) value:0];
		[blit fillBuffer:bucket_buffer range:NSMakeRange(0, bucket_buffer.length) value:0];
		[blit fillBuffer:quant_buffer range:NSMakeRange(0, quant_buffer.length) value:0];
		[blit endEncoding];

		id<MTLComputeCommandEncoder> cmd = [cmd_buf computeCommandEncoderWithDispatchType:MTLDispatchTypeSerial];
		dwt(cmd, *planes);
		quant(cmd);
		analyze_rdo(cmd);
		resolve_rdo(cmd, target_bitstream_size);
		block_packing(cmd);
		[cmd endEncoding];

		return run(cmd_buf);
	}

	// Encoder::Impl::packetize with the whole frame as one packet.
	int packetize(uint8_t *output_bitstream, size_t size, size_t *out_len) const
	{
		auto *meta = static_cast<const BitstreamPacket *>(out_meta.contents);
		auto *input_bitstream = static_cast<const uint32_t *>(out_bitstream.contents);
		const size_t input_words = out_bitstream.length / sizeof(uint32_t);

		size_t num_non_zero_blocks = 0;
		size_t total = sizeof(BitstreamSequenceHeader);
		for (int i = 0; i < block_count_32x32; i++)
		{
			if (meta[i].num_words == 0)
				continue;
			if (size_t(meta[i].offset_u32) + meta[i].num_words > input_words ||
			    meta[i].num_words < sizeof(BitstreamHeader) / sizeof(uint32_t))
				return fail(PWM_ERROR_GPU, "block %d: record %u+%u outside the bitstream buffer", i,
				            meta[i].offset_u32, meta[i].num_words);
			num_non_zero_blocks++;
			total += meta[i].num_words * sizeof(uint32_t);
		}
		if (total > size)
			return fail(PWM_ERROR_SPACE, "frame is %zu bytes, room for %zu", total, size);
		if (size_t(meta[0].offset_u32) + 2 > input_words)
			return fail(PWM_ERROR_GPU, "block 0 outside the bitstream buffer");

		BitstreamSequenceHeader header = {};
		header.width_minus_1 = uint32_t(width - 1);
		header.height_minus_1 = uint32_t(height - 1);
		header.sequence = reinterpret_cast<const BitstreamHeader *>(input_bitstream + meta[0].offset_u32)->sequence;
		header.extended = 1;
		header.code = BITSTREAM_EXTENDED_CODE_START_OF_FRAME;
		header.total_blocks = uint32_t(num_non_zero_blocks);
		header.chroma_resolution = chroma444 ? CHROMA_RESOLUTION_444 : CHROMA_RESOLUTION_420;

		size_t output_offset = 0;
		memcpy(output_bitstream, &header, sizeof(header));
		output_offset += sizeof(header);

		for (int i = 0; i < block_count_32x32; i++)
		{
			size_t packet_size = meta[i].num_words * sizeof(uint32_t);
			if (!packet_size)
				continue;
			memcpy(output_bitstream + output_offset, input_bitstream + meta[i].offset_u32, packet_size);
			output_offset += packet_size;
		}

		*out_len = output_offset;
		return PWM_OK;
	}
};

// ── Decoder (pyrowave_decoder.cpp) ────────────────────────────────────────────

struct DequantizerPushData
{
	ivec2 resolution;
	int32_t output_layer;
	int32_t block_offset_32x32;
	int32_t block_stride_32x32;
};

struct DecoderImpl : WaveletBuffers
{
	id<MTLBuffer> dequant_offset_buffer, payload_data;
	// The iDWT's chroma output (half size for 4:2:0), interleaved into the surface's CbCr plane at
	// the end. 16-bit, so it holds an HDR surface's samples as well as an 8-bit one's.
	id<MTLTexture> chroma_planes[2];
	SurfaceCache surfaces;

	std::vector<uint32_t> dequant_offset_buffer_cpu;
	std::vector<uint32_t> payload_data_cpu;
	int decoded_blocks = 0;
	int total_blocks_in_sequence = 0;
	uint32_t last_seq = UINT32_MAX;

	int chroma_width() const
	{
		return chroma444 ? width : width / 2;
	}
	int chroma_height() const
	{
		return chroma444 ? height : height / 2;
	}

	bool init_decoder_buffers()
	{
		dequant_offset_buffer = shared_buffer(size_t(block_count_32x32) * sizeof(uint32_t));
		dequant_offset_buffer_cpu.resize(size_t(block_count_32x32));
		payload_data_cpu.reserve(1024 * 1024);

		MTLTextureDescriptor *desc = [MTLTextureDescriptor texture2DDescriptorWithPixelFormat:MTLPixelFormatR16Unorm
		                                                                                width:NSUInteger(chroma_width())
		                                                                               height:NSUInteger(chroma_height())
		                                                                            mipmapped:NO];
		desc.usage = MTLTextureUsageShaderRead | MTLTextureUsageShaderWrite;
		desc.storageMode = MTLStorageModePrivate;
		chroma_planes[0] = [device newTextureWithDescriptor:desc];
		chroma_planes[1] = [device newTextureWithDescriptor:desc];
		clear();
		return dequant_offset_buffer && chroma_planes[0] && chroma_planes[1];
	}

	void clear()
	{
		std::fill(dequant_offset_buffer_cpu.begin(), dequant_offset_buffer_cpu.end(), UINT32_MAX);
		decoded_blocks = 0;
		last_seq = UINT32_MAX;
		total_blocks_in_sequence = block_count_32x32;
		payload_data_cpu.clear();
	}

	bool decode_packet(const BitstreamHeader *header)
	{
		auto &offset = dequant_offset_buffer_cpu[header->block_index];
		if (offset == UINT32_MAX)
		{
			decoded_blocks++;
			offset = uint32_t(payload_data_cpu.size());
		}
		else
		{
			return true;
		}

		auto *payload_words = reinterpret_cast<const uint32_t *>(header);

		if (sizeof(*header) / sizeof(uint32_t) > header->payload_words)
		{
			fail(PWM_ERROR_BITSTREAM, "payload_words is not large enough");
			return false;
		}

		payload_data_cpu.insert(payload_data_cpu.end(), payload_words, payload_words + header->payload_words);
		return true;
	}

	bool push_packet(const void *data_, size_t size)
	{
		auto *data = static_cast<const uint8_t *>(data_);
		while (size >= sizeof(BitstreamHeader))
		{
			// The access unit is a byte buffer; the records are word aligned within it.
			BitstreamHeader header_copy;
			memcpy(&header_copy, data, sizeof(header_copy));
			const BitstreamHeader *header = &header_copy;

			if (header->extended != 0)
			{
				BitstreamSequenceHeader seq;
				memcpy(&seq, data, sizeof(seq));

				if (seq.chroma_resolution != (chroma444 ? CHROMA_RESOLUTION_444 : CHROMA_RESOLUTION_420))
				{
					fail(PWM_ERROR_BITSTREAM, "chroma resolution mismatch");
					return false;
				}

				uint8_t diff = (header->sequence - last_seq) & SequenceCountMask;
				if (last_seq != UINT32_MAX && diff > (SequenceCountMask / 2))
					return true;

				if (last_seq == UINT32_MAX || diff != 0)
				{
					clear();
					last_seq = header->sequence;
				}

				if (seq.code == BITSTREAM_EXTENDED_CODE_START_OF_FRAME)
				{
					if (int(seq.width_minus_1) + 1 != width || int(seq.height_minus_1) + 1 != height)
					{
						fail(PWM_ERROR_BITSTREAM, "dimension mismatch in seq packet, (%d, %d) != (%d, %d)",
						     seq.width_minus_1 + 1, seq.height_minus_1 + 1, width, height);
						return false;
					}

					total_blocks_in_sequence = int(seq.total_blocks);
				}
				else
				{
					fail(PWM_ERROR_BITSTREAM, "unrecognized sequence header mode %u", seq.code);
					return false;
				}

				data += sizeof(*header);
				size -= sizeof(*header);
				continue;
			}

			size_t packet_size = header->payload_words * sizeof(uint32_t);

			// A packet must be at least its own header, so this parse cursor always advances
			// (upstream's PUNKTFUNK fix, security-review 2026-08-15 #9).
			if (header->payload_words < sizeof(*header) / sizeof(uint32_t))
			{
				fail(PWM_ERROR_BITSTREAM, "payload_words (%u) is smaller than the packet header", header->payload_words);
				return false;
			}

			if (packet_size > size)
			{
				fail(PWM_ERROR_BITSTREAM, "packet header states %zu bytes, but only %zu bytes left", packet_size, size);
				return false;
			}

			bool restart;

			if (last_seq == UINT32_MAX)
			{
				restart = true;
			}
			else
			{
				uint8_t diff = (header->sequence - last_seq) & SequenceCountMask;
				if (diff > (SequenceCountMask / 2))
					return true;
				restart = diff != 0;
			}

			if (restart)
			{
				clear();
				last_seq = header->sequence;
			}

			if (header->block_index >= uint32_t(block_count_32x32))
			{
				fail(PWM_ERROR_BITSTREAM, "block_index %u is out of bounds (>= %d)", unsigned(header->block_index),
				     block_count_32x32);
				return false;
			}

			// decode_packet reads the whole record through the header pointer.
			if (!decode_packet(reinterpret_cast<const BitstreamHeader *>(data)))
				return false;

			data += packet_size;
			size -= packet_size;
		}

		if (size != 0)
		{
			fail(PWM_ERROR_BITSTREAM, "did not consume the access unit completely");
			return false;
		}

		return true;
	}

	bool upload_payload()
	{
		size_t required_size = payload_data_cpu.size() * sizeof(uint32_t);

		// Avoid edge case OOB access without robustness on the payload buffer during dequant.
		size_t required_size_padded = required_size + 16;

		if (!payload_data || required_size_padded > payload_data.length)
		{
			payload_data = shared_buffer(std::max<size_t>(64 * 1024, required_size_padded * 2));
			if (!payload_data)
				return false;
		}

		auto *dst = static_cast<uint8_t *>(payload_data.contents);
		if (required_size)
			memcpy(dst, payload_data_cpu.data(), required_size);
		memset(dst + required_size, 0, 16);
		memcpy(dequant_offset_buffer.contents, dequant_offset_buffer_cpu.data(),
		       dequant_offset_buffer_cpu.size() * sizeof(uint32_t));
		return true;
	}

	void dequant(id<MTLComputeCommandEncoder> cmd)
	{
		[cmd setComputePipelineState:pipelines->wavelet_dequant];
		[cmd setBuffer:dequant_offset_buffer offset:0 atIndex:1];
		[cmd setBuffer:payload_data offset:0 atIndex:2];

		for (int level = 0; level < DecompositionLevels; level++)
		{
			for (int component = 0; component < NumComponents; component++)
			{
				// Ignore top-level CbCr when doing 420 subsampling.
				if (skip_band_level(level, component))
					continue;

				[cmd setTexture:component_layer_views[component][level] atIndex:0];

				for (int band = (level == DecompositionLevels - 1 ? 0 : 1); band < 4; band++)
				{
					DequantizerPushData push = {};
					push.resolution = { level_width(level), level_height(level) };
					push.output_layer = band;
					push.block_offset_32x32 = block_meta[component][level][band].block_offset_32x32;
					push.block_stride_32x32 = block_meta[component][level][band].block_stride_32x32;
					set_push(cmd, &push, sizeof(push));

					dispatch(cmd, NSUInteger((push.resolution.x + 31) / 32), NSUInteger((push.resolution.y + 31) / 32), 128);
				}
			}
		}
	}

	void idwt(id<MTLComputeCommandEncoder> cmd, id<MTLTexture> y_plane)
	{
		struct
		{
			ivec2 resolution;
			vec2 inv_resolution;
		} push = {};

		[cmd setSamplerState:mirror_repeat_sampler atIndex:0];

		for (int input_level = DecompositionLevels - 1; input_level >= 0; input_level--)
		{
			// Transposed.
			push.resolution = { level_height(input_level), level_width(input_level) };
			push.inv_resolution = { 1.0f / float(push.resolution.x), 1.0f / float(push.resolution.y) };
			set_push(cmd, &push, sizeof(push));

			NSUInteger groups_x = NSUInteger((push.resolution.x + 15) / 16);
			NSUInteger groups_y = NSUInteger((push.resolution.y + 15) / 16);

			if (input_level == 0)
			{
				[cmd setComputePipelineState:pipelines->idwt[1]];
				for (int c = 0; c < (chroma444 ? NumComponents : 1); c++)
				{
					[cmd setTexture:(c == 0 ? y_plane : chroma_planes[c - 1]) atIndex:1];
					[cmd setTexture:component_layer_views[c][input_level] atIndex:0];
					dispatch(cmd, groups_x, groups_y, 64);
				}
			}
			else
			{
				for (int c = 0; c < NumComponents; c++)
				{
					[cmd setTexture:component_layer_views[c][input_level] atIndex:0];

					if (!chroma444 && c != 0 && input_level == 1)
					{
						[cmd setTexture:chroma_planes[c - 1] atIndex:1];
						[cmd setComputePipelineState:pipelines->idwt[1]];
					}
					else
					{
						[cmd setTexture:component_ll_views[c][input_level - 1] atIndex:1];
						[cmd setComputePipelineState:pipelines->idwt[0]];
					}

					dispatch(cmd, groups_x, groups_y, 64);
				}
			}
		}
	}

	int decode(const uint8_t *au, size_t len, IOSurfaceRef surface)
	{
		const SurfaceTextures *planes = surfaces.get(device, surface, width, height, chroma444, true);
		if (!planes)
			return PWM_ERROR_SURFACE;

		// One access unit is one whole frame: anything left from an earlier one is stale.
		clear();
		if (!push_packet(au, len))
			return PWM_ERROR_BITSTREAM;
		if (last_seq == UINT32_MAX || decoded_blocks < total_blocks_in_sequence)
			return fail(PWM_ERROR_INCOMPLETE, "%d of %d blocks", decoded_blocks, total_blocks_in_sequence);
		if (!upload_payload())
			return fail(PWM_ERROR_GPU, "allocating the payload buffer failed");

		id<MTLCommandBuffer> cmd_buf = [queue commandBuffer];
		id<MTLComputeCommandEncoder> cmd = [cmd_buf computeCommandEncoderWithDispatchType:MTLDispatchTypeSerial];
		dequant(cmd);
		idwt(cmd, planes->y);

		[cmd setComputePipelineState:pipelines->cbcr_interleave];
		[cmd setTexture:chroma_planes[0] atIndex:0];
		[cmd setTexture:chroma_planes[1] atIndex:1];
		[cmd setTexture:planes->cbcr atIndex:2];
		[cmd dispatchThreads:MTLSizeMake(NSUInteger(chroma_width()), NSUInteger(chroma_height()), 1)
		    threadsPerThreadgroup:MTLSizeMake(16, 16, 1)];
		[cmd endEncoding];

		return run(cmd_buf);
	}
};
} // namespace

struct pwm_encoder
{
	EncoderImpl impl;
};

struct pwm_decoder
{
	DecoderImpl impl;
};

extern "C" {

const char *pwm_last_error(void)
{
	return last_error.c_str();
}

int pwm_supported(void *mtl_device)
{
	last_error.clear();
	@autoreleasepool
	{
		id<MTLDevice> device = resolve_device(mtl_device);
		if (!device)
		{
			fail(PWM_ERROR_UNSUPPORTED, "no Metal device");
			return 0;
		}
		return make_pipeline(device, kPwmShader_cbcr_interleave, "cbcr_interleave", nil, 64) != nil;
	}
}

int pwm_encoder_create(void *mtl_device, int width, int height, int chroma, pwm_encoder **out)
{
	last_error.clear();
	if (!out)
		return fail(PWM_ERROR_ARGUMENT, "null out pointer");
	@autoreleasepool
	{
		auto *enc = new pwm_encoder();
		int r = enc->impl.init(mtl_device, width, height, chroma, true);
		if (r == PWM_OK && !enc->impl.init_encoder_buffers())
			r = fail(PWM_ERROR_GPU, "allocating the encoder buffers failed");
		if (r != PWM_OK)
		{
			delete enc;
			return r;
		}
		*out = enc;
		return PWM_OK;
	}
}

void pwm_encoder_destroy(pwm_encoder *enc)
{
	@autoreleasepool
	{
		delete enc;
	}
}

int pwm_encoder_encode(pwm_encoder *enc, void *iosurface, size_t target_size,
                       uint8_t *out, size_t out_capacity, size_t *out_len)
{
	last_error.clear();
	if (!enc || !iosurface || !out || !out_len)
		return fail(PWM_ERROR_ARGUMENT, "null argument");
	@autoreleasepool
	{
		int r = enc->impl.encode(static_cast<IOSurfaceRef>(iosurface), target_size);
		if (r != PWM_OK)
			return r;
		return enc->impl.packetize(out, out_capacity, out_len);
	}
}

uint32_t pwm_encoder_last_gpu_us(const pwm_encoder *enc)
{
	return enc ? enc->impl.last_gpu_us : 0;
}

int pwm_decoder_create(void *mtl_device, int width, int height, int chroma, pwm_decoder **out)
{
	last_error.clear();
	if (!out)
		return fail(PWM_ERROR_ARGUMENT, "null out pointer");
	@autoreleasepool
	{
		auto *dec = new pwm_decoder();
		int r = dec->impl.init(mtl_device, width, height, chroma, false);
		if (r == PWM_OK && !dec->impl.init_decoder_buffers())
			r = fail(PWM_ERROR_GPU, "allocating the decoder buffers failed");
		if (r != PWM_OK)
		{
			delete dec;
			return r;
		}
		*out = dec;
		return PWM_OK;
	}
}

void pwm_decoder_destroy(pwm_decoder *dec)
{
	@autoreleasepool
	{
		delete dec;
	}
}

int pwm_decoder_decode(pwm_decoder *dec, const uint8_t *au, size_t len, void *iosurface)
{
	last_error.clear();
	if (!dec || !au || !iosurface)
		return fail(PWM_ERROR_ARGUMENT, "null argument");
	@autoreleasepool
	{
		return dec->impl.decode(au, len, static_cast<IOSurfaceRef>(iosurface));
	}
}

uint32_t pwm_decoder_last_gpu_us(const pwm_decoder *dec)
{
	return dec ? dec->impl.last_gpu_us : 0;
}

} // extern "C"
