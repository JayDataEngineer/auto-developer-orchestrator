# OmniParser (Microsoft)

**Repo**: `reference/OmniParser/` — General-purpose screen parser using YOLO + OCR + Florence-2

## What It Is

Microsoft's vision-only screen parsing pipeline. Detects interactive elements (buttons, icons, inputs) using YOLO v8, captions them with Florence-2, reads text with EasyOCR/PaddleOCR, then merges everything with smart overlap resolution. The gold standard for desktop element detection.

## Key Insights

### 1. Three-Stage Pipeline
```
1. OCR (EasyOCR + PaddleOCR) → text regions with bounding boxes
2. YOLO v8 → interactive element icons (buttons, checkboxes, inputs)
3. Florence-2 → natural language captions for icons ("search icon", "close button")
```

### 2. Smart Overlap Resolution
- IoU threshold: 0.7
- OCR text detection prioritized over icon detection
- Meaning: if YOLO says there's an icon where OCR found text, OCR wins
- This is correct — text is usually more actionable than generic icons

### 3. Batch Icon Captioning
- 128 icons captioned at once by Florence-2
- ~0.25 seconds for 128 icons on GPU
- Makes the model practical for real-time use

### 4. Coordinate Normalization
- All bounding boxes as [0, 1] ratios (not pixels)
- `x_ratio * image_width` at action time
- Box overlay thickness scales by `max(image.size) / 3200`

### 5. Element Classification
```json
[
  {"type": "text", "bbox": [0.1, 0.2, 0.3, 0.1], "interactivity": false, "content": "Search"},
  {"type": "icon", "bbox": [0.4, 0.2, 0.05, 0.05], "interactivity": true, "content": "search icon"}
]
```
- Separates **interactive** (icons, buttons) from **non-interactive** (text labels)
- Not everything with a bounding box should be clicked

### 6. VRAM Requirements
- Florence-2: ~4GB GPU VRAM
- This runs **alongside** llama.cpp on an RTX 4090 (24GB total)
- YOLO is lightweight (~1GB)
- Tradeoff: 5GB for screen parsing vs more context space for the LLM

## What We've Implemented

Nothing from OmniParser yet. Our desktop mode relies entirely on xdotool coordinates with no element detection.

## Gaps

| Priority | Feature | Effort | Why |
|----------|---------|--------|-----|
| P0 | Any form of desktop element detection | High | Currently blind on desktop — model guesses coordinates from vision alone |
| P1 | OCR text detection for desktop apps | Medium | Even just tesseract in the sandbox would be a big improvement |
| P1 | Overlap deduplication (IoU-based) | Low | If we do any multi-method detection, we need this |
| P2 | Batch icon captioning with vision model | High | Requires separate vision model. Our llama.cpp has vision support (mmproj loaded) |
| P3 | Full YOLO + Florence-2 pipeline | Very High | 5GB VRAM cost. Only justified if desktop is primary use case |
| P3 | Element interactivity classification | Medium | Useful but requires vision model |

### Key Architectural Insight
OmniParser's insight isn't just about using vision models — it's that **separating text from icons and merging with smart overlap resolution** is the right approach. We should start with OCR-only (cheap, high impact), then consider adding icon detection if desktop automation proves valuable.

A pragmatic first step: run tesseract in the Docker sandbox, add an `ocr_screenshot` tool that returns text elements with bounding boxes normalized to 0-1. That alone would be a massive improvement over blind xdotool clicks.
