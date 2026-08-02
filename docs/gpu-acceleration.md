# GoVid — FFmpeg GPU Acceleration

> **Status:** backend targets identified; capability detection and GPU pipelines are not yet implemented.
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

---

## 5. Pipeline design constraints

- Hardware decode alone may not improve a CPU-filtered job. Downloading frames to system memory can erase the gain.
- Hardware filters usually require a specific pixel format and frames resident on the correct device.
- Crossing device APIs within one pipeline should be avoided unless benchmarks show a benefit.
- Audio filters remain on the CPU; the target backends apply only to video processing and encoding.
- Codec availability is hardware-dependent. H.264 is the safest baseline; HEVC, AV1, 10-bit formats, and chroma subsampling require separate capability flags.
- GPU output must preserve GoVid's current temporary-file and replace-on-success behavior.
- Explicit backend selection should report why it is unavailable, then follow the project's defined fallback policy.

---

## 6. Recommended implementation order

1. Inventory the bundled FFmpeg builds using the four capability commands above.
2. Add typed backend and capability models in Go.
3. Implement runtime probes and cache results for the current FFmpeg binary, device, and driver environment.
4. Accelerate final video encoding first, because it has clear boundaries and CPU equivalents.
5. Add GPU scaling where frames can remain on one device for the full video path.
6. Evaluate tone mapping and denoise independently; keep them on CPU until quality, format support, and transfer overhead are understood.
7. Benchmark representative 1080p, 1440p, and 4K jobs before enabling any backend in `auto` mode.

Complex existing filters such as `nlmeans`, `deshake`, `deband`, and audio processing should remain CPU-based initially. Mixed GPU/CPU filter graphs are valid, but they should be selected only when measured performance justifies the upload and download overhead.

---

## 7. Definition of done for backend identification

- Target device, decode, filter, and encode APIs are named for Windows, Linux, and macOS.
- Initial platform priorities and stable configuration identifiers are defined.
- Compile-time FFmpeg capability checks are distinguished from runtime device probes.
- CPU fallback remains a requirement for every accelerated path.
- The next work item can verify these targets against the actual bundled FFmpeg builds.