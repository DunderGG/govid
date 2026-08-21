# GoVid — FFmpeg GPU Acceleration

> **Status:** backend targets, bundled-build capabilities, and feature scope identified; runtime capability detection and the encode-only command builder are implemented, wired to a user-facing setting, and protected by a strict CPU fallback on runtime failure; GPU scale/deinterlace pipelines are not yet implemented.
> **Audience:** contributors and maintainers working on FFmpeg post-processing.

---

## 1. Goal

GoVid should use hardware acceleration when it is available and beneficial, without making GPU support a requirement. Every accelerated operation must have a CPU equivalent, and a failed GPU initialization must be able to fall back to that CPU path.

An acceleration backend represents a complete media pipeline, not only an encoder. Depending on the operation, a pipeline can include:

- hardware device initialization;
- hardware decoding;
- frame upload to or download from GPU memory;
- hardware filters; and
- hardware encoding.

For example, CUDA provides NVIDIA device, decode, and filter support, while NVENC provides NVIDIA encoding.

---

## 2. Target backend matrix

| OS | Vendor | Device / decode / filters | Encode | GoVid priority | Notes |
|---|---|---|---|---|---|
| Windows | NVIDIA | CUDA, D3D11VA | NVENC | Primary | CUDA enables NVIDIA-specific filters; D3D11VA may be useful for decode-only paths. |
| Windows | Intel | QSV, D3D11VA | QSV | Primary | Prefer QSV for an end-to-end Intel pipeline. |
| Windows | AMD | D3D11VA | AMF | Primary | AMF is primarily the encoder; D3D11VA supplies hardware decode. |
| Linux | NVIDIA | CUDA | NVENC | Primary | Requires an FFmpeg build with NVIDIA codec support and a compatible driver. |
| Linux | Intel | VAAPI, QSV | VAAPI, QSV | Primary | Start with VAAPI for broad support; add specialized QSV paths where they provide a clear benefit. |
| Linux | AMD | VAAPI | VAAPI | Primary | VAAPI is the normal Linux acceleration interface for AMD hardware. |
| macOS | Apple | VideoToolbox | VideoToolbox | Primary | Use the native Apple framework; exact codec support varies by hardware and macOS version. |

DXVA2 can remain a compatibility option on older Windows systems, but new pipeline work should prefer D3D11VA. Vulkan is not an initial backend target; it may later be useful for cross-vendor filters such as scaling or tone mapping if the bundled FFmpeg build and driver support are reliable.

---

## 3. Initial platform scope

The first implementation should target:

| Platform | Initial targets |
|---|---|
| Windows | NVIDIA CUDA/NVENC, Intel QSV, AMD D3D11VA/AMF |
| Linux | NVIDIA CUDA/NVENC and VAAPI; QSV as a specialized Intel path |
| macOS | VideoToolbox |

CPU processing remains the universal fallback on every platform.

Suggested stable configuration identifiers are:

| Identifier | Meaning |
|---|---|
| `auto` | Detect capabilities and select the best usable backend. |
| `off` | Disable GPU acceleration. |
| `nvidia` | Select the CUDA/NVENC pipeline. |
| `intel` | Select QSV where supported. |
| `amd` | Select D3D11VA/AMF on Windows. |
| `vaapi` | Select the Linux VAAPI pipeline. |
| `videotoolbox` | Select the macOS VideoToolbox pipeline. |

These identifiers describe user intent. Internally, capability records should retain the individual decoder, filter, and encoder features because a machine may support only part of a pipeline.

---

## 4. Capability policy

GoVid must not infer support only from the operating system or GPU vendor. A backend is usable only when both checks succeed:

1. The active FFmpeg binary reports the required components.
2. A small runtime probe successfully initializes the device and requested codec or filter.

This distinction matters because `ffmpeg -hwaccels` reports acceleration methods compiled into FFmpeg, not whether the current machine, driver, and device can actually use them.

The bundled FFmpeg binary should be inspected with:

```text
ffmpeg -hide_banner -hwaccels
ffmpeg -hide_banner -encoders
ffmpeg -hide_banner -decoders
ffmpeg -hide_banner -filters
```

Relevant names include `cuda`, `nvenc`, `qsv`, `amf`, `vaapi`, `d3d11va`, `vulkan`, and `videotoolbox`. Checks should use exact parsed component names rather than loose substring matching.

The runtime probe encodes a single synthetic frame (`probeEncoder` in `gpu_capability.go`) at 320x240, not a smaller size: several hardware encoders (notably NVENC) reject resolutions below their minimum supported size with an "Invalid argument" (`EINVAL`) error, which would otherwise be misread as the encoder/GPU being unavailable rather than a probe artifact.

---

## 5. Current bundled build inventory

Verified on 2026-08-02 against the Windows binary packaged from `external/ffmpeg.exe`:

| Property | Value |
|---|---|
| FFmpeg version | `8.1-essentials_build-www.gyan.dev` |
| SHA-256 | `1A65D5B0B10D8D9A81D2824A3538046A40ED3607C906B335A166ADD87613F705` |
| Build source | gyan.dev essentials build |

### Reported hardware APIs

The binary reports `cuda`, `qsv`, `amf`, `d3d11va`, `d3d12va`, `dxva2`, and `vaapi` from `-hwaccels`.

| Backend | Encoders | Decoders | Relevant filters | Build status |
|---|---|---|---|---|
| NVIDIA | `h264_nvenc`, `hevc_nvenc`, `av1_nvenc` | CUVID support for AV1, H.264, HEVC, MJPEG, MPEG-1/2/4, VC-1, VP8, and VP9 | `scale_cuda`, `colorspace_cuda`, `bilateral_cuda`, `bwdif_cuda`, `yadif_cuda`, `hwupload_cuda`, and others | Compiled in |
| Intel QSV | H.264, HEVC, AV1, VP9, MPEG-2, and MJPEG | AV1, H.264, HEVC, MJPEG, MPEG-2, VC-1, VP8, VP9, and VVC | `scale_qsv`, `vpp_qsv`, `deinterlace_qsv`, overlay, pad, and stack filters | Compiled in |
| AMD AMF | `h264_amf`, `hevc_amf`, `av1_amf` | `av1_amf`, `h264_amf`, `hevc_amf`, `vp9_amf` | `vpp_amf`, `sr_amf` | Compiled in |
| VAAPI | H.264, HEVC, AV1, VP8, VP9, MPEG-2, and MJPEG | Reported as a hardware API; codec use is selected through `-hwaccel vaapi` | Scaling, tone mapping, denoise, sharpness, deinterlace, color adjustment, overlay, pad, and stack filters | Compiled in; not a primary Windows path |
| VideoToolbox | None | None | None | Not compiled in, as expected for Windows |
| Vulkan / OpenCL | None reported by the targeted inventory | None reported by the targeted inventory | None reported by the targeted inventory | Not available in this build |

This confirms that the current package contains the components needed to attempt the three primary Windows pipelines. It does not confirm that any pipeline works on the current or an end user's GPU. Driver and device initialization belong to runtime capability detection.

Windows is currently the only platform with a bundled artifact in this repository. Repeat this inventory and record a platform-specific binary checksum when macOS and Linux packages are added.

---

## 6. Pipeline design constraints

- Hardware decode alone may not improve a CPU-filtered job. Downloading frames to system memory can erase the gain.
- Hardware filters usually require a specific pixel format and frames resident on the correct device.
- Crossing device APIs within one pipeline should be avoided unless benchmarks show a benefit.
- Audio filters remain on the CPU; the target backends apply only to video processing and encoding.
- Codec availability is hardware-dependent. H.264 is the safest baseline; HEVC, AV1, 10-bit formats, and chroma subsampling require separate capability flags.
- GPU output must preserve GoVid's current temporary-file and replace-on-success behavior.
- Explicit backend selection should report why it is unavailable, then follow the project's defined fallback policy. *(Implemented — see `PPEngine.retryWithCPU` in `pp_engine.go`: if a GPU-encoded job exits non-zero, `runJob` rebuilds the job with `buildFFmpegArgsForBackend(..., BackendOff)`, logs the ffmpeg failure reason via the shared `lastLine` helper, and retries once with CPU before falling through to the normal failure path. The retry is silent to the failure-tracking UI — `cb.OnFailure()` only fires if the CPU retry itself fails — so a successful fallback does not surface the "Retry" button.)*

---

## 7. Feature scope decision

Decided 2026-08-20, scoped against the checkbox filters built by `buildPostProcessFilters` in `postprocess.go`, using the bundled-build inventory in §5.

| Feature (UI checkbox) | FFmpeg filter(s) | Scope this phase | Reason |
|---|---|---|---|
| Final video encode | `libx264` / `libvpx-vp9` | **GPU** | Swapping only the `-c:v` encoder needs no change to the existing `-vf` graph; every accelerated backend accepts software frames directly for encode. Benefits every post-processing job regardless of which other filters are active. |
| `upscaleVideo` | `scale` (lanczos) | Deferred | Cross-vendor GPU equivalents exist (`scale_cuda`, `scale_qsv`, `scale_vaapi`) but only work cleanly in a full hardware pipeline when scaling is the sole active filter; needs `-hwaccel`/pixel-format plumbing not yet designed. |
| `deinterlace` | `bwdif` | Deferred | Cross-vendor GPU equivalents exist (`bwdif_cuda`, `deinterlace_qsv`, `deinterlace_vaapi`) with the same sole-active-filter constraint as `upscaleVideo`. |
| `denoise` | `nlmeans` / `hqdn3d` | CPU (stays) | Only a VAAPI-specific equivalent (`denoise_vaapi`) exists in the bundled build; not cross-vendor. Matches the CPU-first guidance in §6. |
| `hdrToSdr` | `zscale`/`tonemap` | CPU (stays) | Only a VAAPI-specific equivalent (`tonemap_vaapi`) exists; this filter is already flagged as unreliable in the roadmap, so added complexity is avoided for now. |
| `sharpen` | `cas` | CPU (stays) | No direct GPU equivalent; VAAPI's `sharpness_vaapi` is a different algorithm and single-vendor. |
| `vividMode` | `eq` | CPU (stays) | No direct GPU equivalent; VAAPI's `procamp_vaapi` is single-vendor and not equivalent. |
| `deband` | `deband` | CPU (stays) | No GPU equivalent in the bundled build. |
| `stabilize` | `deshake` | CPU (stays) | No GPU equivalent in the bundled build. |
| `autoCrop` | `cropdetect` + `crop` | CPU (stays) | Already a low-cost operation; not worth the pipeline complexity. |
| `normalizeAudio` | `loudnorm` | CPU (stays) | Audio filter; target backends in this document apply to video only. |
| `nightMode` | `dynaudnorm` | CPU (stays) | Audio filter; target backends in this document apply to video only. |

### Final-encode mapping

| Output | Current CPU encoder | `nvidia` | `intel` | `amd` | `vaapi` | `videotoolbox` |
|---|---|---|---|---|---|---|
| MP4/MKV (H.264) | `libx264 -crf 18 -preset slower` | `h264_nvenc` | `h264_qsv` | `h264_amf` | `h264_vaapi` | `h264_videotoolbox` |
| WebM (VP9) | `libvpx-vp9 -crf 31 -b:v 0 -deadline good -cpu-used 2` | stays CPU | stays CPU | stays CPU | stays CPU | stays CPU |

WebM output stays on the CPU VP9 encoder in this phase: the bundled build has no NVENC or AMF VP9 encoder, so a GPU path would only cover VAAPI/QSV and produce inconsistent behavior across vendors.

Hardware encoder settings should target visual parity with the current `-crf 18 -preset slower` baseline, using each vendor's constant-quality mode (`h264_nvenc -rc constqp -qp <n>`, `h264_qsv -global_quality <n>`, `h264_amf -qp_i/-qp_p <n>`, `h264_vaapi -qp <n>`, `h264_videotoolbox -q:v <n>`). Exact numeric values are left to the benchmarking step in §8, not fixed here.

Implemented in `gpu_capability.go` as `PlanEncoder(requested GPUBackend, capabilities map[GPUBackend]BackendCapability, containerExt string) EncoderPlan` — always returns a runnable `-c:v` argument set, falling back to the CPU encoder whenever the requested/auto backend isn't `Available`, matching `.webm`, or is `off`. `PPEngine`'s `GPUBackend`/`GPUCapabilities` fields are now wired from a "Encoder Backend" selector in the Post-Processing window (`ui.gpuBackend`, persisted as the `gpuBackend` preference) and `GPUCapabilityService.Detect` in `postprocess.go`'s `applyFFmpegFilters`. `GPUBackendOptions`/`GPUBackendFromLabel` map the selector's OS-filtered labels ("Auto (Recommended)", "Off", "NVIDIA", "Intel", "AMD", "VAAPI", "VideoToolbox") to `GPUBackend` values.

---

## 8. Recommended implementation order

1. Inventory the bundled FFmpeg builds using the four capability commands above.
2. Add typed backend and capability models in Go.
3. Implement runtime probes and cache results for the current FFmpeg binary, device, and driver environment. *(Done — see `GPUCapabilityService`, `BackendCapability`, and `Detect` in `gpu_capability.go`. Detection covers the H.264 final-encode path only, in-memory for the current app run; not yet wired into the encode path.)*
4. Accelerate final video encoding first, because it has clear boundaries and CPU equivalents. *(Done — see `PlanEncoder`/`EncoderPlan` in `gpu_capability.go`, wired via the "Encoder Backend" setting in the Post-Processing window. Fallback-on-failure and cross-run benchmarking still pending.)*
5. Add GPU scaling where frames can remain on one device for the full video path.
6. Evaluate tone mapping and denoise independently; keep them on CPU until quality, format support, and transfer overhead are understood.
7. Benchmark representative 1080p, 1440p, and 4K jobs before enabling any backend in `auto` mode.

Complex existing filters such as `nlmeans`, `deshake`, `deband`, and audio processing should remain CPU-based initially. Mixed GPU/CPU filter graphs are valid, but they should be selected only when measured performance justifies the upload and download overhead.

---

## 9. Definition of done for backend identification

- Target device, decode, filter, and encode APIs are named for Windows, Linux, and macOS.
- Initial platform priorities and stable configuration identifiers are defined.
- Compile-time FFmpeg capability checks are distinguished from runtime device probes.
- CPU fallback remains a requirement for every accelerated path.
- The next work item can verify these targets against the actual bundled FFmpeg builds.

---

## 10. Runtime guardrails

Implemented in `pp_engine.go`, addressing failure modes observed beyond a simple nonzero ffmpeg exit:

- **Concurrent hardware encoder session cap** — `PPEngine.gpuSem`, a buffered channel sized `maxConcurrentGPUJobs` (2), bounds how many GPU-encoded jobs run simultaneously within a single `ApplyFilters` batch. Consumer GPUs (NVENC in particular) enforce a low concurrent session limit; without this cap, a batch with more workers than that limit would see most jobs fail on the GPU and silently retry to CPU, largely negating the speedup. CPU-only jobs are unaffected and do not touch the semaphore.
- **Stall watchdog** — `runJob` starts a `time.AfterFunc` timer (`gpuStallTimeout`, 30s) only for GPU-encoded jobs, reset on every line of ffmpeg output. If a hung driver stops producing output entirely for that long, the process is killed and `retryWithCPU` takes over, instead of the batch hanging indefinitely. CPU jobs are not subject to this timeout.
- The GPU semaphore slot and watchdog are released *before* a CPU retry begins (not merely via `defer` at function return), so the retry's CPU-only encode does not needlessly hold a GPU session slot for its entire duration.

Not yet covered: an explicit driver-mismatch/out-of-memory classification (currently folded into the generic retry-on-any-failure path), and HDR/10-bit pixel-format compatibility checks ahead of encode (currently relies on the CPU filter chain already normalizing to `yuv420p` where applicable, plus the generic retry as a safety net).