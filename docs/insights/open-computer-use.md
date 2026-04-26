# open-computer-use

**Repo**: `reference/open-computer-use/` — Coasty's multi-agent desktop automation

## What It Is

A pragmatic, Docker-native desktop automation framework. Best known for its 4-tier screenshot fallback chain, 3-method CV element detection with priority merging, and X server recovery in containerized environments. Also has the most aggressive prompt engineering of any reference.

## Key Insights

### 1. 4-Tier Screenshot Fallback
```
1. mss (fastest, primary)
2. pyautogui
3. scrot
4. ImageMagick import
```
- Each method tried with 5-second timeout
- Auto `recover_display()` on X server errors
- XAUTHORITY and DISPLAY env vars handled per method
- This is the pattern to follow for desktop screenshot reliability

### 2. 3-Method CV Element Detection with Priority Merging
```
Priority 3 (highest): OCR text detection (pytesseract with preprocessing)
Priority 2: Color-based clickable detection (HSV ranges for blue/green/red/gray buttons)
Priority 1 (lowest): Contour-based UI element detection (Canny edge + findContours)
```
- Merge: Sort by priority, overlap >50% -> keep higher priority
- Element format: `"{method}_{type}_{index}"` with center coordinates

### 3. Image Preprocessing for OCR
```python
denoise (fastNlMeansDenoising) -> CLAHE contrast -> adaptive threshold -> dilation -> sharpen
```
- All done server-side before OCR
- Significantly improves tesseract accuracy

### 4. X Server Recovery
- `recover_display()` for Docker environments
- Auto-restarts Xvfb/Xorg if display fails
- Critical for headless containerized desktop

### 5. Anti-Detection
- Random viewport sizes
- User agent rotation
- Evasion tactics for bot detection

### 6. Aggressive Prompt Engineering
- "BE PROACTIVE: Complete tasks without asking permission"
- "MAKE DECISIONS: Use context and defaults when details missing"
- "NO HEDGING: State facts clearly without unnecessary qualifiers"
- "Understand -> Act -> Answer, skip explanations"
- These prompts consistently outperform polite/ cautious prompts in benchmarks

### 7. Command-Specific Timeouts
- Click: 30s
- Screenshot: 60s
- Type: varies by text length
- Not one-size-fits-all timeout

## What We've Implemented

Minimal. We have xdotool-based desktop automation but no fallback chains, no CV detection, no anti-detection.

## Gaps

| Priority | Feature | Effort | Why |
|----------|---------|--------|-----|
| P0 | Multi-method screenshot fallback | Low | Single xdotool method is fragile. Even a 2-tier fallback helps |
| P1 | OCR text detection for desktop apps | Medium | pytesseract in the sandbox with image preprocessing |
| P1 | Aggressive prompt engineering | Low | Compare our prompt tone to theirs — we might be too cautious |
| P1 | Image preprocessing (denoise, CLAHE, threshold) | Low | Preprocessing pipeline before OCR and before sending to LLM |
| P1 | Command-specific timeout configuration | Low | Different operations need different timeouts |
| P2 | CV-based element detection (color + contour) | Medium | Works without ML models, useful as fallback |
| P2 | X server recovery in Docker | Low | Our sandbox environment is similar to theirs |
| P3 | Anti-detection (random viewport, UA rotation) | Low | Only relevant for web scraping use cases |

### Key Architectural Insight
open-computer-use is the most pragmatic reference for desktop automation in Docker. Its key insight is that **reliability comes from redundancy** — not a single perfect method but a chain of fallbacks. We should start with the screenshot fallback chain (cheapest, highest impact) and then layer on OCR.
