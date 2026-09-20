// vk_overlay_common.h -- Net Graph HUD + AI Vision overlay compositing shared
// by the Vulkan renderers (vk_video_impl_linux.c today; vk_video_impl_windows.c
// carries an older private copy of the same logic that can be switched to
// this header once verified on a Windows build).
//
// Plain #include into ONE translation unit, not a standalone module. The
// includer must, before including this file, provide:
//   - Vulkan globals/helpers: g_dev, g_pdev, g_swap_fmt, vk_find_mem(),
//     vk_image_barrier(), vk_shader_from_spv(), goVKLog(),
//     and g_hud_vert_spv/g_hud_frag_spv (shader_arrays.h)
//   - VKOV_MUTEX_DECL: declares the overlay handoff lock (guards the pixel
//     buffers/dirty flags between Go goroutines and the render thread)
//   - VKOV_LOCK() / VKOV_UNLOCK(): lock primitives for that lock
// Exposes (for cgo): vk_hud_set_pixels/clear/set_scale,
// vk_aivision_set_pixels/clear. Render-thread entry points:
// vk_hud_maybe_upload_cmds/vk_hud_record_draw,
// vk_aivision_maybe_upload_cmds/vk_aivision_record_draw, vk_hud_destroy.

#ifndef VK_OVERLAY_COMMON_H
#define VK_OVERLAY_COMMON_H

VKOV_MUTEX_DECL

// ─── Net Graph HUD + AI Vision overlay (native GPU compositing) ──────────────
// Frames decoded straight into a VkImage never exist as CPU pixels, so the
// RGBA path's burn-in (goNetGraphOverlay/goAIVisionOverlay) can't touch them. Instead net_graph.go's ~10Hz canvas
// (netGraphCachedImg, VK_HUD_W x VK_HUD_H RGBA) is pushed here via
// vk_hud_set_pixels into a small persistent texture, and drawn as a second
// alpha-blended quad after the video draw in the same dynamic-rendering
// pass. Ported from vk_video_impl_windows.c's HUD path; VK_HUD_W/H/MARGIN
// must match net_graph.go's netGraphCanvasW/H/netGraphHudMargin by hand.
#define VK_HUD_W      640
#define VK_HUD_H      400
#define VK_HUD_MARGIN 24

static VkPipeline            g_hud_pipeline  = VK_NULL_HANDLE;
static VkPipelineLayout      g_hud_playout   = VK_NULL_HANDLE;
static VkDescriptorSetLayout g_hud_dsl       = VK_NULL_HANDLE;
static VkDescriptorPool      g_hud_dpool     = VK_NULL_HANDLE;
static VkDescriptorSet       g_hud_dset      = VK_NULL_HANDLE;
static VkSampler             g_hud_sampler   = VK_NULL_HANDLE;
static VkImage               g_hud_tex       = VK_NULL_HANDLE;
static VkDeviceMemory        g_hud_tex_mem   = VK_NULL_HANDLE;
static VkImageView           g_hud_tex_view  = VK_NULL_HANDLE;
static VkImageLayout         g_hud_tex_layout = VK_IMAGE_LAYOUT_UNDEFINED;
static VkBuffer              g_hud_stage_buf = VK_NULL_HANDLE;
static VkDeviceMemory        g_hud_stage_mem = VK_NULL_HANDLE;
static void                 *g_hud_stage_ptr = NULL;
static int                   g_hud_resources_ok = 0; // 0=not tried, 1=ready, -1=failed

static uint8_t         g_hud_pixels[VK_HUD_W * VK_HUD_H * 4];
static int             g_hud_dirty  = 0; // g_hud_pixels newer than g_hud_tex
static int             g_hud_active = 0; // Net Graph enabled (set on push, cleared on vk_hud_clear)
static atomic_int      g_hud_scale_pct = 100;

// AI Vision overlay: full-frame-sized counterpart to the HUD, sharing its
// sampler/layout/pipeline/pool (own descriptor set + texture, resized to the
// live video resolution since detection boxes live in that pixel space).
static VkDescriptorSet g_aivision_dset      = VK_NULL_HANDLE;
static VkImage         g_aivision_tex       = VK_NULL_HANDLE;
static VkDeviceMemory  g_aivision_tex_mem   = VK_NULL_HANDLE;
static VkImageView     g_aivision_tex_view  = VK_NULL_HANDLE;
static VkImageLayout   g_aivision_tex_layout = VK_IMAGE_LAYOUT_UNDEFINED;
static int             g_aivision_tex_w = 0, g_aivision_tex_h = 0;
static VkBuffer        g_aivision_stage_buf = VK_NULL_HANDLE;
static VkDeviceMemory  g_aivision_stage_mem = VK_NULL_HANDLE;
static void           *g_aivision_stage_ptr = NULL;
static VkDeviceSize    g_aivision_stage_sz  = 0;
// Cross-thread handoff (Go detection goroutine -> render thread), g_hud_mu.
static uint8_t        *g_aivision_pixels = NULL;
static size_t          g_aivision_pixels_sz = 0;
static int             g_aivision_pending_w = 0, g_aivision_pending_h = 0;
static int             g_aivision_dirty  = 0;
static int             g_aivision_active = 0;

// vk_aivision_set_pixels: called from Go once per completed detection pass
// (~0.5-2Hz) with a w x h RGBA canvas (transparent, boxes/tags opaque).
// Copy + flag only; the GPU upload happens lazily on the render thread.
int vk_aivision_set_pixels(const uint8_t *rgba, int w, int h) {
    if (w <= 0 || h <= 0) return 0;
    size_t sz = (size_t)w * (size_t)h * 4;
    VKOV_LOCK();
    if (!g_aivision_pixels || g_aivision_pixels_sz < sz) {
        uint8_t *grown = (uint8_t *)realloc(g_aivision_pixels, sz);
        if (!grown) { VKOV_UNLOCK(); return 0; }
        g_aivision_pixels = grown;
        g_aivision_pixels_sz = sz;
    }
    memcpy(g_aivision_pixels, rgba, sz);
    g_aivision_pending_w = w; g_aivision_pending_h = h;
    g_aivision_dirty  = 1;
    g_aivision_active = 1;
    VKOV_UNLOCK();
    return 1;
}

void vk_aivision_clear(void) {
    VKOV_LOCK();
    g_aivision_active = 0;
    g_aivision_dirty  = 0;
    VKOV_UNLOCK();
}

// vk_hud_set_pixels: called from Go's net_graph tick (~10Hz) with the
// freshly built canvas. Only copies + flags dirty -- no Vulkan calls, safe
// from any goroutine and before/after the renderer exists.
int vk_hud_set_pixels(const uint8_t *rgba, int w, int h) {
    if (w != VK_HUD_W || h != VK_HUD_H) {
        goVKLog("vk_hud_set_pixels: HUD canvas size mismatch (net_graph.go's netGraphCanvasW/H changed?)", 2);
        return 0;
    }
    VKOV_LOCK();
    memcpy(g_hud_pixels, rgba, sizeof(g_hud_pixels));
    g_hud_dirty  = 1;
    g_hud_active = 1;
    VKOV_UNLOCK();
    return 1;
}

void vk_hud_clear(void) {
    VKOV_LOCK();
    g_hud_active = 0;
    g_hud_dirty  = 0;
    VKOV_UNLOCK();
}

void vk_hud_set_scale(float s) {
    if (s < 0.25f) s = 0.25f;
    if (s > 2.0f)  s = 2.0f;
    atomic_store(&g_hud_scale_pct, (int)(s * 100.0f + 0.5f));
}

static void vk_hud_destroy(void) {
    if (g_hud_stage_ptr && g_hud_stage_mem) { vkUnmapMemory(g_dev, g_hud_stage_mem); g_hud_stage_ptr = NULL; }
    if (g_hud_stage_buf) { vkDestroyBuffer(g_dev, g_hud_stage_buf, NULL); g_hud_stage_buf = VK_NULL_HANDLE; }
    if (g_hud_stage_mem) { vkFreeMemory(g_dev, g_hud_stage_mem, NULL);    g_hud_stage_mem = VK_NULL_HANDLE; }
    if (g_hud_tex_view)  { vkDestroyImageView(g_dev, g_hud_tex_view, NULL); g_hud_tex_view = VK_NULL_HANDLE; }
    if (g_hud_tex)       { vkDestroyImage(g_dev, g_hud_tex, NULL);          g_hud_tex = VK_NULL_HANDLE; }
    if (g_hud_tex_mem)   { vkFreeMemory(g_dev, g_hud_tex_mem, NULL);        g_hud_tex_mem = VK_NULL_HANDLE; }
    if (g_hud_pipeline)  { vkDestroyPipeline(g_dev, g_hud_pipeline, NULL);  g_hud_pipeline = VK_NULL_HANDLE; }
    if (g_hud_playout)   { vkDestroyPipelineLayout(g_dev, g_hud_playout, NULL); g_hud_playout = VK_NULL_HANDLE; }
    if (g_hud_dpool)     { vkDestroyDescriptorPool(g_dev, g_hud_dpool, NULL); g_hud_dpool = VK_NULL_HANDLE; }
    g_hud_dset = VK_NULL_HANDLE;
    g_aivision_dset = VK_NULL_HANDLE;
    if (g_aivision_stage_ptr && g_aivision_stage_mem) { vkUnmapMemory(g_dev, g_aivision_stage_mem); g_aivision_stage_ptr = NULL; }
    if (g_aivision_stage_buf) { vkDestroyBuffer(g_dev, g_aivision_stage_buf, NULL); g_aivision_stage_buf = VK_NULL_HANDLE; }
    if (g_aivision_stage_mem) { vkFreeMemory(g_dev, g_aivision_stage_mem, NULL);    g_aivision_stage_mem = VK_NULL_HANDLE; }
    g_aivision_stage_sz = 0;
    if (g_aivision_tex_view) { vkDestroyImageView(g_dev, g_aivision_tex_view, NULL); g_aivision_tex_view = VK_NULL_HANDLE; }
    if (g_aivision_tex)      { vkDestroyImage(g_dev, g_aivision_tex, NULL);          g_aivision_tex = VK_NULL_HANDLE; }
    if (g_aivision_tex_mem)  { vkFreeMemory(g_dev, g_aivision_tex_mem, NULL);        g_aivision_tex_mem = VK_NULL_HANDLE; }
    g_aivision_tex_layout = VK_IMAGE_LAYOUT_UNDEFINED;
    g_aivision_tex_w = g_aivision_tex_h = 0;
    if (g_hud_dsl)       { vkDestroyDescriptorSetLayout(g_dev, g_hud_dsl, NULL); g_hud_dsl = VK_NULL_HANDLE; }
    if (g_hud_sampler)   { vkDestroySampler(g_dev, g_hud_sampler, NULL); g_hud_sampler = VK_NULL_HANDLE; }
    g_hud_tex_layout = VK_IMAGE_LAYOUT_UNDEFINED;
    g_hud_resources_ok = 0;
    // A fresh renderer's texture is empty -- if Net Graph is still on, make
    // the next canvas push (or the cached one) re-upload.
    VKOV_LOCK();
    if (g_hud_active) g_hud_dirty = 1;
    if (g_aivision_active && g_aivision_pixels) g_aivision_dirty = 1;
    VKOV_UNLOCK();
}

// vk_hud_ensure_resources lazily creates the HUD's sampler/layout/pipeline/
// texture/staging buffer. Returns 1 once ready, 0 on failure (permanent).
static int vk_hud_ensure_resources(void) {
    if (g_hud_resources_ok) return g_hud_resources_ok > 0;
    g_hud_resources_ok = -1;

    VkSamplerCreateInfo sampCI = { VK_STRUCTURE_TYPE_SAMPLER_CREATE_INFO };
    sampCI.magFilter = VK_FILTER_LINEAR;
    sampCI.minFilter = VK_FILTER_LINEAR;
    sampCI.addressModeU = VK_SAMPLER_ADDRESS_MODE_CLAMP_TO_EDGE;
    sampCI.addressModeV = VK_SAMPLER_ADDRESS_MODE_CLAMP_TO_EDGE;
    sampCI.addressModeW = VK_SAMPLER_ADDRESS_MODE_CLAMP_TO_EDGE;
    if (vkCreateSampler(g_dev, &sampCI, NULL, &g_hud_sampler) != VK_SUCCESS) goto fail;

    {
        VkDescriptorSetLayoutBinding binding = {0};
        binding.binding = 0;
        binding.descriptorType = VK_DESCRIPTOR_TYPE_COMBINED_IMAGE_SAMPLER;
        binding.descriptorCount = 1;
        binding.stageFlags = VK_SHADER_STAGE_FRAGMENT_BIT;
        binding.pImmutableSamplers = &g_hud_sampler;
        VkDescriptorSetLayoutCreateInfo dslCI = { VK_STRUCTURE_TYPE_DESCRIPTOR_SET_LAYOUT_CREATE_INFO };
        dslCI.bindingCount = 1; dslCI.pBindings = &binding;
        if (vkCreateDescriptorSetLayout(g_dev, &dslCI, NULL, &g_hud_dsl) != VK_SUCCESS) goto fail;
    }
    {
        VkPushConstantRange pcr = { VK_SHADER_STAGE_VERTEX_BIT, 0, sizeof(float) * 4 };
        VkPipelineLayoutCreateInfo plCI = { VK_STRUCTURE_TYPE_PIPELINE_LAYOUT_CREATE_INFO };
        plCI.setLayoutCount = 1; plCI.pSetLayouts = &g_hud_dsl;
        plCI.pushConstantRangeCount = 1; plCI.pPushConstantRanges = &pcr;
        if (vkCreatePipelineLayout(g_dev, &plCI, NULL, &g_hud_playout) != VK_SUCCESS) goto fail;
    }
    {
        VkDescriptorPoolSize poolSize = { VK_DESCRIPTOR_TYPE_COMBINED_IMAGE_SAMPLER, 2 };
        VkDescriptorPoolCreateInfo poolCI = { VK_STRUCTURE_TYPE_DESCRIPTOR_POOL_CREATE_INFO };
        poolCI.maxSets = 2; poolCI.poolSizeCount = 1; poolCI.pPoolSizes = &poolSize;
        if (vkCreateDescriptorPool(g_dev, &poolCI, NULL, &g_hud_dpool) != VK_SUCCESS) goto fail;
        VkDescriptorSetAllocateInfo dsai = { VK_STRUCTURE_TYPE_DESCRIPTOR_SET_ALLOCATE_INFO };
        dsai.descriptorPool = g_hud_dpool; dsai.descriptorSetCount = 1; dsai.pSetLayouts = &g_hud_dsl;
        if (vkAllocateDescriptorSets(g_dev, &dsai, &g_hud_dset) != VK_SUCCESS) goto fail;
        if (vkAllocateDescriptorSets(g_dev, &dsai, &g_aivision_dset) != VK_SUCCESS) goto fail;
    }
    {
        VkShaderModule vs = vk_shader_from_spv(g_hud_vert_spv, sizeof(g_hud_vert_spv));
        VkShaderModule fs = vk_shader_from_spv(g_hud_frag_spv, sizeof(g_hud_frag_spv));
        if (!vs || !fs) {
            if (vs) vkDestroyShaderModule(g_dev, vs, NULL);
            if (fs) vkDestroyShaderModule(g_dev, fs, NULL);
            goto fail;
        }
        VkPipelineShaderStageCreateInfo stages[2] = {0};
        stages[0].sType = VK_STRUCTURE_TYPE_PIPELINE_SHADER_STAGE_CREATE_INFO;
        stages[0].stage = VK_SHADER_STAGE_VERTEX_BIT; stages[0].module = vs; stages[0].pName = "main";
        stages[1].sType = VK_STRUCTURE_TYPE_PIPELINE_SHADER_STAGE_CREATE_INFO;
        stages[1].stage = VK_SHADER_STAGE_FRAGMENT_BIT; stages[1].module = fs; stages[1].pName = "main";

        VkPipelineVertexInputStateCreateInfo vi = { VK_STRUCTURE_TYPE_PIPELINE_VERTEX_INPUT_STATE_CREATE_INFO };
        VkPipelineInputAssemblyStateCreateInfo ia = { VK_STRUCTURE_TYPE_PIPELINE_INPUT_ASSEMBLY_STATE_CREATE_INFO };
        ia.topology = VK_PRIMITIVE_TOPOLOGY_TRIANGLE_LIST;
        VkPipelineViewportStateCreateInfo vpState = { VK_STRUCTURE_TYPE_PIPELINE_VIEWPORT_STATE_CREATE_INFO };
        vpState.viewportCount = 1; vpState.scissorCount = 1;
        VkDynamicState dynStates[2] = { VK_DYNAMIC_STATE_VIEWPORT, VK_DYNAMIC_STATE_SCISSOR };
        VkPipelineDynamicStateCreateInfo dynCI = { VK_STRUCTURE_TYPE_PIPELINE_DYNAMIC_STATE_CREATE_INFO };
        dynCI.dynamicStateCount = 2; dynCI.pDynamicStates = dynStates;
        VkPipelineRasterizationStateCreateInfo rs = { VK_STRUCTURE_TYPE_PIPELINE_RASTERIZATION_STATE_CREATE_INFO };
        rs.polygonMode = VK_POLYGON_MODE_FILL; rs.cullMode = VK_CULL_MODE_NONE; rs.lineWidth = 1.0f;
        VkPipelineMultisampleStateCreateInfo ms = { VK_STRUCTURE_TYPE_PIPELINE_MULTISAMPLE_STATE_CREATE_INFO };
        ms.rasterizationSamples = VK_SAMPLE_COUNT_1_BIT;
        VkPipelineColorBlendAttachmentState cba = {0};
        cba.blendEnable = VK_TRUE;
        cba.srcColorBlendFactor = VK_BLEND_FACTOR_SRC_ALPHA;
        cba.dstColorBlendFactor = VK_BLEND_FACTOR_ONE_MINUS_SRC_ALPHA;
        cba.colorBlendOp = VK_BLEND_OP_ADD;
        cba.srcAlphaBlendFactor = VK_BLEND_FACTOR_ONE;
        cba.dstAlphaBlendFactor = VK_BLEND_FACTOR_ONE_MINUS_SRC_ALPHA;
        cba.alphaBlendOp = VK_BLEND_OP_ADD;
        cba.colorWriteMask = VK_COLOR_COMPONENT_R_BIT | VK_COLOR_COMPONENT_G_BIT | VK_COLOR_COMPONENT_B_BIT | VK_COLOR_COMPONENT_A_BIT;
        VkPipelineColorBlendStateCreateInfo cb = { VK_STRUCTURE_TYPE_PIPELINE_COLOR_BLEND_STATE_CREATE_INFO };
        cb.attachmentCount = 1; cb.pAttachments = &cba;
        VkPipelineRenderingCreateInfo renderingCI = { VK_STRUCTURE_TYPE_PIPELINE_RENDERING_CREATE_INFO };
        renderingCI.colorAttachmentCount = 1; renderingCI.pColorAttachmentFormats = &g_swap_fmt;

        VkGraphicsPipelineCreateInfo pipeCI = { VK_STRUCTURE_TYPE_GRAPHICS_PIPELINE_CREATE_INFO, &renderingCI };
        pipeCI.stageCount = 2; pipeCI.pStages = stages;
        pipeCI.pVertexInputState = &vi; pipeCI.pInputAssemblyState = &ia;
        pipeCI.pViewportState = &vpState; pipeCI.pRasterizationState = &rs;
        pipeCI.pMultisampleState = &ms; pipeCI.pColorBlendState = &cb;
        pipeCI.pDynamicState = &dynCI;
        pipeCI.layout = g_hud_playout;
        VkResult pr = vkCreateGraphicsPipelines(g_dev, VK_NULL_HANDLE, 1, &pipeCI, NULL, &g_hud_pipeline);
        vkDestroyShaderModule(g_dev, vs, NULL);
        vkDestroyShaderModule(g_dev, fs, NULL);
        if (pr != VK_SUCCESS) goto fail;
    }
    {
        VkImageCreateInfo ici = { VK_STRUCTURE_TYPE_IMAGE_CREATE_INFO };
        ici.imageType = VK_IMAGE_TYPE_2D;
        ici.format = VK_FORMAT_R8G8B8A8_UNORM;
        ici.extent = (VkExtent3D){ VK_HUD_W, VK_HUD_H, 1 };
        ici.mipLevels = 1; ici.arrayLayers = 1;
        ici.samples = VK_SAMPLE_COUNT_1_BIT;
        ici.tiling = VK_IMAGE_TILING_OPTIMAL;
        ici.usage = VK_IMAGE_USAGE_SAMPLED_BIT | VK_IMAGE_USAGE_TRANSFER_DST_BIT;
        ici.initialLayout = VK_IMAGE_LAYOUT_UNDEFINED;
        if (vkCreateImage(g_dev, &ici, NULL, &g_hud_tex) != VK_SUCCESS) goto fail;
        VkMemoryRequirements mr;
        vkGetImageMemoryRequirements(g_dev, g_hud_tex, &mr);
        VkPhysicalDeviceMemoryProperties mp;
        vkGetPhysicalDeviceMemoryProperties(g_pdev, &mp);
        uint32_t mi = vk_find_mem(&mp, mr.memoryTypeBits, VK_MEMORY_PROPERTY_DEVICE_LOCAL_BIT);
        if (mi == UINT32_MAX) goto fail;
        VkMemoryAllocateInfo mai = { VK_STRUCTURE_TYPE_MEMORY_ALLOCATE_INFO };
        mai.allocationSize = mr.size; mai.memoryTypeIndex = mi;
        if (vkAllocateMemory(g_dev, &mai, NULL, &g_hud_tex_mem) != VK_SUCCESS) goto fail;
        vkBindImageMemory(g_dev, g_hud_tex, g_hud_tex_mem, 0);

        VkImageViewCreateInfo vci = { VK_STRUCTURE_TYPE_IMAGE_VIEW_CREATE_INFO };
        vci.image = g_hud_tex; vci.viewType = VK_IMAGE_VIEW_TYPE_2D; vci.format = VK_FORMAT_R8G8B8A8_UNORM;
        vci.subresourceRange.aspectMask = VK_IMAGE_ASPECT_COLOR_BIT;
        vci.subresourceRange.levelCount = 1; vci.subresourceRange.layerCount = 1;
        if (vkCreateImageView(g_dev, &vci, NULL, &g_hud_tex_view) != VK_SUCCESS) goto fail;

        VkDescriptorImageInfo imgInfo = { VK_NULL_HANDLE, g_hud_tex_view, VK_IMAGE_LAYOUT_SHADER_READ_ONLY_OPTIMAL };
        VkWriteDescriptorSet write = { VK_STRUCTURE_TYPE_WRITE_DESCRIPTOR_SET };
        write.dstSet = g_hud_dset; write.dstBinding = 0; write.descriptorCount = 1;
        write.descriptorType = VK_DESCRIPTOR_TYPE_COMBINED_IMAGE_SAMPLER;
        write.pImageInfo = &imgInfo;
        vkUpdateDescriptorSets(g_dev, 1, &write, 0, NULL);
    }
    {
        VkBufferCreateInfo bci = { VK_STRUCTURE_TYPE_BUFFER_CREATE_INFO };
        bci.size = (VkDeviceSize)(VK_HUD_W * VK_HUD_H * 4);
        bci.usage = VK_BUFFER_USAGE_TRANSFER_SRC_BIT; bci.sharingMode = VK_SHARING_MODE_EXCLUSIVE;
        if (vkCreateBuffer(g_dev, &bci, NULL, &g_hud_stage_buf) != VK_SUCCESS) goto fail;
        VkMemoryRequirements mr;
        vkGetBufferMemoryRequirements(g_dev, g_hud_stage_buf, &mr);
        VkPhysicalDeviceMemoryProperties mp;
        vkGetPhysicalDeviceMemoryProperties(g_pdev, &mp);
        uint32_t mi = vk_find_mem(&mp, mr.memoryTypeBits,
            VK_MEMORY_PROPERTY_HOST_VISIBLE_BIT | VK_MEMORY_PROPERTY_HOST_COHERENT_BIT);
        if (mi == UINT32_MAX) goto fail;
        VkMemoryAllocateInfo mai = { VK_STRUCTURE_TYPE_MEMORY_ALLOCATE_INFO };
        mai.allocationSize = mr.size; mai.memoryTypeIndex = mi;
        if (vkAllocateMemory(g_dev, &mai, NULL, &g_hud_stage_mem) != VK_SUCCESS) goto fail;
        vkBindBufferMemory(g_dev, g_hud_stage_buf, g_hud_stage_mem, 0);
        vkMapMemory(g_dev, g_hud_stage_mem, 0, VK_WHOLE_SIZE, 0, &g_hud_stage_ptr);
    }

    g_hud_resources_ok = 1;
    VKOV_LOCK();
    if (g_hud_active) g_hud_dirty = 1;
    VKOV_UNLOCK();
    return 1;

fail:
    goVKLog("vk_hud_ensure_resources: failed -- Net Graph HUD will not render", 2);
    vk_hud_destroy();
    g_hud_resources_ok = -1;
    return 0;
}

// vk_hud_maybe_upload_cmds records a staging->texture copy into cb if a new
// canvas was pushed since the last frame. Must run after
// vkBeginCommandBuffer and before vkCmdBeginRendering (transfers aren't
// valid inside a rendering pass). No GPU work at all when nothing changed --
// nearly every call, since pushes are ~10Hz vs the video frame rate. Also
// returns whether the HUD should be drawn this frame.
static int vk_hud_maybe_upload_cmds(VkCommandBuffer cb) {
    VKOV_LOCK();
    int active = g_hud_active;
    VKOV_UNLOCK();
    if (!active) return 0;
    if (!vk_hud_ensure_resources()) return 0;

    int have_new = 0;
    VKOV_LOCK();
    if (g_hud_dirty) {
        memcpy(g_hud_stage_ptr, g_hud_pixels, sizeof(g_hud_pixels));
        g_hud_dirty = 0;
        have_new = 1;
    }
    VKOV_UNLOCK();

    if (have_new) {
        VkImageLayout old_layout = g_hud_tex_layout;
        vk_image_barrier(cb, g_hud_tex, old_layout, VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL,
            old_layout == VK_IMAGE_LAYOUT_UNDEFINED ? 0 : VK_ACCESS_SHADER_READ_BIT,
            VK_ACCESS_TRANSFER_WRITE_BIT,
            old_layout == VK_IMAGE_LAYOUT_UNDEFINED ? VK_PIPELINE_STAGE_TOP_OF_PIPE_BIT : VK_PIPELINE_STAGE_FRAGMENT_SHADER_BIT,
            VK_PIPELINE_STAGE_TRANSFER_BIT);
        VkBufferImageCopy bic = {0};
        bic.bufferRowLength = VK_HUD_W;
        bic.bufferImageHeight = VK_HUD_H;
        bic.imageSubresource.aspectMask = VK_IMAGE_ASPECT_COLOR_BIT;
        bic.imageSubresource.layerCount = 1;
        bic.imageExtent = (VkExtent3D){ VK_HUD_W, VK_HUD_H, 1 };
        vkCmdCopyBufferToImage(cb, g_hud_stage_buf, g_hud_tex, VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL, 1, &bic);
        vk_image_barrier(cb, g_hud_tex, VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL, VK_IMAGE_LAYOUT_SHADER_READ_ONLY_OPTIMAL,
            VK_ACCESS_TRANSFER_WRITE_BIT, VK_ACCESS_SHADER_READ_BIT,
            VK_PIPELINE_STAGE_TRANSFER_BIT, VK_PIPELINE_STAGE_FRAGMENT_SHADER_BIT);
        g_hud_tex_layout = VK_IMAGE_LAYOUT_SHADER_READ_ONLY_OPTIMAL;
    }
    // Nothing to draw until the first canvas has ever been uploaded.
    return g_hud_tex_layout == VK_IMAGE_LAYOUT_SHADER_READ_ONLY_OPTIMAL;
}

// vk_hud_record_draw draws the HUD quad anchored to the bottom-right of the
// fw x fh video frame (native resolution, matching net_graph.go's
// netGraphBlitOverlay). Call between vkCmdBeginRendering/EndRendering with
// the video's letterboxed viewport/scissor still bound, so NDC 0..1 of the
// frame maps onto the picture rather than the whole window.
static void vk_hud_record_draw(VkCommandBuffer cb, int fw, int fh) {
    if (fw <= 0 || fh <= 0) return;
    int pct = atomic_load(&g_hud_scale_pct);
    if (pct < 25) pct = 25;
    if (pct > 200) pct = 200;
    float dw = (float)VK_HUD_W * ((float)pct / 100.0f);
    float dh = (float)VK_HUD_H * ((float)pct / 100.0f);
    float hx0 = (float)fw - (float)VK_HUD_MARGIN - dw;
    if (hx0 < (float)VK_HUD_MARGIN) hx0 = (float)VK_HUD_MARGIN;
    float hy0 = (float)fh - (float)VK_HUD_MARGIN - dh;
    if (hy0 < (float)VK_HUD_MARGIN) hy0 = (float)VK_HUD_MARGIN;
    float rect[4] = {
        (hx0 / (float)fw) * 2.0f - 1.0f,        (hy0 / (float)fh) * 2.0f - 1.0f,
        ((hx0 + dw) / (float)fw) * 2.0f - 1.0f, ((hy0 + dh) / (float)fh) * 2.0f - 1.0f,
    };
    vkCmdBindPipeline(cb, VK_PIPELINE_BIND_POINT_GRAPHICS, g_hud_pipeline);
    vkCmdBindDescriptorSets(cb, VK_PIPELINE_BIND_POINT_GRAPHICS, g_hud_playout, 0, 1, &g_hud_dset, 0, NULL);
    vkCmdPushConstants(cb, g_hud_playout, VK_SHADER_STAGE_VERTEX_BIT, 0, sizeof(rect), rect);
    vkCmdDraw(cb, 6, 1, 0, 0);
}

// vk_aivision_ensure_tex (re)creates AI Vision's texture/view/staging buffer
// when the requested size changes. Runs on the render thread after this
// frame's fence wait, so the previous frame's GPU work has retired and the
// old objects are safe to destroy. Returns 1 once ready at (w, h).
static int vk_aivision_ensure_tex(int w, int h) {
    if (!vk_hud_ensure_resources()) return 0;
    if (g_aivision_tex != VK_NULL_HANDLE && g_aivision_tex_w == w && g_aivision_tex_h == h) return 1;

    if (g_aivision_tex_view) { vkDestroyImageView(g_dev, g_aivision_tex_view, NULL); g_aivision_tex_view = VK_NULL_HANDLE; }
    if (g_aivision_tex)      { vkDestroyImage(g_dev, g_aivision_tex, NULL);          g_aivision_tex = VK_NULL_HANDLE; }
    if (g_aivision_tex_mem)  { vkFreeMemory(g_dev, g_aivision_tex_mem, NULL);        g_aivision_tex_mem = VK_NULL_HANDLE; }
    g_aivision_tex_layout = VK_IMAGE_LAYOUT_UNDEFINED;
    g_aivision_tex_w = g_aivision_tex_h = 0;

    VkImageCreateInfo ici = { VK_STRUCTURE_TYPE_IMAGE_CREATE_INFO };
    ici.imageType = VK_IMAGE_TYPE_2D;
    ici.format = VK_FORMAT_R8G8B8A8_UNORM;
    ici.extent = (VkExtent3D){ (uint32_t)w, (uint32_t)h, 1 };
    ici.mipLevels = 1; ici.arrayLayers = 1;
    ici.samples = VK_SAMPLE_COUNT_1_BIT;
    ici.tiling = VK_IMAGE_TILING_OPTIMAL;
    ici.usage = VK_IMAGE_USAGE_SAMPLED_BIT | VK_IMAGE_USAGE_TRANSFER_DST_BIT;
    ici.initialLayout = VK_IMAGE_LAYOUT_UNDEFINED;
    if (vkCreateImage(g_dev, &ici, NULL, &g_aivision_tex) != VK_SUCCESS) return 0;

    VkPhysicalDeviceMemoryProperties mp;
    vkGetPhysicalDeviceMemoryProperties(g_pdev, &mp);
    VkMemoryRequirements mr;
    vkGetImageMemoryRequirements(g_dev, g_aivision_tex, &mr);
    uint32_t mi = vk_find_mem(&mp, mr.memoryTypeBits, VK_MEMORY_PROPERTY_DEVICE_LOCAL_BIT);
    if (mi == UINT32_MAX) return 0;
    VkMemoryAllocateInfo mai = { VK_STRUCTURE_TYPE_MEMORY_ALLOCATE_INFO };
    mai.allocationSize = mr.size; mai.memoryTypeIndex = mi;
    if (vkAllocateMemory(g_dev, &mai, NULL, &g_aivision_tex_mem) != VK_SUCCESS) return 0;
    vkBindImageMemory(g_dev, g_aivision_tex, g_aivision_tex_mem, 0);

    VkImageViewCreateInfo vci = { VK_STRUCTURE_TYPE_IMAGE_VIEW_CREATE_INFO };
    vci.image = g_aivision_tex; vci.viewType = VK_IMAGE_VIEW_TYPE_2D; vci.format = VK_FORMAT_R8G8B8A8_UNORM;
    vci.subresourceRange.aspectMask = VK_IMAGE_ASPECT_COLOR_BIT;
    vci.subresourceRange.levelCount = 1; vci.subresourceRange.layerCount = 1;
    if (vkCreateImageView(g_dev, &vci, NULL, &g_aivision_tex_view) != VK_SUCCESS) return 0;

    VkDescriptorImageInfo imgInfo = { VK_NULL_HANDLE, g_aivision_tex_view, VK_IMAGE_LAYOUT_SHADER_READ_ONLY_OPTIMAL };
    VkWriteDescriptorSet write = { VK_STRUCTURE_TYPE_WRITE_DESCRIPTOR_SET };
    write.dstSet = g_aivision_dset; write.dstBinding = 0; write.descriptorCount = 1;
    write.descriptorType = VK_DESCRIPTOR_TYPE_COMBINED_IMAGE_SAMPLER;
    write.pImageInfo = &imgInfo;
    vkUpdateDescriptorSets(g_dev, 1, &write, 0, NULL);

    VkDeviceSize sz = (VkDeviceSize)w * (VkDeviceSize)h * 4;
    if (g_aivision_stage_buf == VK_NULL_HANDLE || g_aivision_stage_sz < sz) {
        if (g_aivision_stage_buf != VK_NULL_HANDLE) {
            vkUnmapMemory(g_dev, g_aivision_stage_mem);
            vkFreeMemory(g_dev, g_aivision_stage_mem, NULL); g_aivision_stage_mem = VK_NULL_HANDLE;
            vkDestroyBuffer(g_dev, g_aivision_stage_buf, NULL); g_aivision_stage_buf = VK_NULL_HANDLE;
            g_aivision_stage_ptr = NULL; g_aivision_stage_sz = 0;
        }
        VkBufferCreateInfo bci = { VK_STRUCTURE_TYPE_BUFFER_CREATE_INFO };
        bci.size = sz; bci.usage = VK_BUFFER_USAGE_TRANSFER_SRC_BIT; bci.sharingMode = VK_SHARING_MODE_EXCLUSIVE;
        if (vkCreateBuffer(g_dev, &bci, NULL, &g_aivision_stage_buf) != VK_SUCCESS) return 0;
        VkMemoryRequirements smr;
        vkGetBufferMemoryRequirements(g_dev, g_aivision_stage_buf, &smr);
        uint32_t smi = vk_find_mem(&mp, smr.memoryTypeBits,
            VK_MEMORY_PROPERTY_HOST_VISIBLE_BIT | VK_MEMORY_PROPERTY_HOST_COHERENT_BIT);
        if (smi == UINT32_MAX) return 0;
        VkMemoryAllocateInfo smai = { VK_STRUCTURE_TYPE_MEMORY_ALLOCATE_INFO };
        smai.allocationSize = smr.size; smai.memoryTypeIndex = smi;
        if (vkAllocateMemory(g_dev, &smai, NULL, &g_aivision_stage_mem) != VK_SUCCESS) return 0;
        vkBindBufferMemory(g_dev, g_aivision_stage_buf, g_aivision_stage_mem, 0);
        vkMapMemory(g_dev, g_aivision_stage_mem, 0, VK_WHOLE_SIZE, 0, &g_aivision_stage_ptr);
        g_aivision_stage_sz = sz;
    }
    g_aivision_tex_w = w; g_aivision_tex_h = h;
    return 1;
}

// vk_aivision_maybe_upload_cmds: counterpart to vk_hud_maybe_upload_cmds --
// records the staging->texture copy when Go published a fresh canvas.
// Must run after vkBeginCommandBuffer and before vkCmdBeginRendering.
static void vk_aivision_maybe_upload_cmds(VkCommandBuffer cb) {
    int have_new = 0, w = 0, h = 0;
    VKOV_LOCK();
    if (g_aivision_active && g_aivision_dirty && g_aivision_pixels) {
        w = g_aivision_pending_w; h = g_aivision_pending_h;
        have_new = 1;
    }
    VKOV_UNLOCK();
    if (!have_new) return;
    if (!vk_aivision_ensure_tex(w, h)) return;

    VKOV_LOCK();
    if (g_aivision_dirty && g_aivision_pending_w == w && g_aivision_pending_h == h) {
        memcpy(g_aivision_stage_ptr, g_aivision_pixels, (size_t)w * (size_t)h * 4);
        g_aivision_dirty = 0;
    } else {
        have_new = 0; // size changed again mid-upload -- next frame
    }
    VKOV_UNLOCK();
    if (!have_new) return;

    VkImageLayout old_layout = g_aivision_tex_layout;
    vk_image_barrier(cb, g_aivision_tex, old_layout, VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL,
        old_layout == VK_IMAGE_LAYOUT_UNDEFINED ? 0 : VK_ACCESS_SHADER_READ_BIT,
        VK_ACCESS_TRANSFER_WRITE_BIT,
        old_layout == VK_IMAGE_LAYOUT_UNDEFINED ? VK_PIPELINE_STAGE_TOP_OF_PIPE_BIT : VK_PIPELINE_STAGE_FRAGMENT_SHADER_BIT,
        VK_PIPELINE_STAGE_TRANSFER_BIT);
    VkBufferImageCopy bic = {0};
    bic.bufferRowLength = (uint32_t)w;
    bic.bufferImageHeight = (uint32_t)h;
    bic.imageSubresource.aspectMask = VK_IMAGE_ASPECT_COLOR_BIT;
    bic.imageSubresource.layerCount = 1;
    bic.imageExtent = (VkExtent3D){ (uint32_t)w, (uint32_t)h, 1 };
    vkCmdCopyBufferToImage(cb, g_aivision_stage_buf, g_aivision_tex, VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL, 1, &bic);
    vk_image_barrier(cb, g_aivision_tex, VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL, VK_IMAGE_LAYOUT_SHADER_READ_ONLY_OPTIMAL,
        VK_ACCESS_TRANSFER_WRITE_BIT, VK_ACCESS_SHADER_READ_BIT,
        VK_PIPELINE_STAGE_TRANSFER_BIT, VK_PIPELINE_STAGE_FRAGMENT_SHADER_BIT);
    g_aivision_tex_layout = VK_IMAGE_LAYOUT_SHADER_READ_ONLY_OPTIMAL;
}

// vk_aivision_record_draw covers the whole fw x fh video frame. Skipped when
// the texture size doesn't match the live frame (stale boxes after a
// resolution change would be misaligned until the next detection pass).
static void vk_aivision_record_draw(VkCommandBuffer cb, int fw, int fh) {
    VKOV_LOCK();
    int active = g_aivision_active;
    VKOV_UNLOCK();
    if (!active || g_hud_resources_ok <= 0) return;
    if (g_aivision_tex == VK_NULL_HANDLE || g_aivision_tex_w != fw || g_aivision_tex_h != fh) return;
    if (g_aivision_tex_layout != VK_IMAGE_LAYOUT_SHADER_READ_ONLY_OPTIMAL) return;
    float rect[4] = { -1.0f, -1.0f, 1.0f, 1.0f };
    vkCmdBindPipeline(cb, VK_PIPELINE_BIND_POINT_GRAPHICS, g_hud_pipeline);
    vkCmdBindDescriptorSets(cb, VK_PIPELINE_BIND_POINT_GRAPHICS, g_hud_playout, 0, 1, &g_aivision_dset, 0, NULL);
    vkCmdPushConstants(cb, g_hud_playout, VK_SHADER_STAGE_VERTEX_BIT, 0, sizeof(rect), rect);
    vkCmdDraw(cb, 6, 1, 0, 0);
}

#endif // VK_OVERLAY_COMMON_H
