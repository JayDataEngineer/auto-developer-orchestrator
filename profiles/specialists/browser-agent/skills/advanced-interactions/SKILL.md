---
name: advanced-interactions
description: >-
  Drag-and-drop (html5 vs physics strategy selection), hover-revealed menus,
  keyboard hotkeys, coordinate clicking for canvas/charts, scroll_into_view for
  off-screen elements, a11y tree for dense pages, iframe entry/exit, and canvas
  pixel-buffer reading. Read when basic click/type can't reach the target or
  when you need to verify a canvas actually painted.
---

# Advanced Interactions

These cover what plain click/type can't — drag, hover-revealed menus,
non-character keys, off-screen elements, iframes, and dense pages.

## Drag-and-drop (browser_drag)

Give a source (index/selector/x,y) and a target (index/selector/x,y, or a
`dx`/`dy` offset). `strategy` defaults to `auto`. **Always verify in the
returned screenshot that the drag worked** (the item moved, the list reordered,
the slider value changed). If `auto` picked wrong and nothing moved, retry once
with the other strategy:

- `html5` — synthetic `dragstart`→`dragover`→`drop`. Best for sortable lists,
  Kanban boards, react-dnd/dnd-kit/SortableJS, file drop-zones.
- `physics` — `mousedown`→`mousemove(N)→`mouseup`. Best for sliders, canvas
  drags, jQuery-UI-style draggables.
- Sliders: either `browser_drag` with a `dx` offset from the thumb, or nudge
  with `browser_press` `ArrowLeft`/`ArrowRight` (often more reliable).

## Hover (browser_hover)

Reveals dropdown menus, tooltips, fly-out panels, and hover-cards. Many nav
menu items have no SoM label until you hover the parent — hover it,
re-screenshot, THEN click the revealed child.

## Press / hotkeys (browser_press)

Send `Enter`, `Escape`, `Tab`, `ArrowDown`, `Control+a`, `Shift+ArrowDown`,
etc. Use for submitting a form field (`Enter`), dismissing a modal (`Escape`),
keyboard-navigating a combobox/menu, or selecting-all before overwriting. For
plain printable text use `browser_type`, not `browser_press`.

## Click at coordinates (browser_click_at)

When the target has no SoM label and no clean CSS selector — a canvas, a chart
point, an image map, a custom-drawn control — click the pixel position you read
off the screenshot. Also does right-click (`right=true`) and double-click
(`double=true`).

## Off-screen elements (browser_scroll_into_view)

When you KNOW an element exists (index/selector) but it's below the fold,
scroll it into view first; its SoM label is then fresh and clickable. More
precise than `browser_scroll` for one specific element.

## Dense pages (browser_a11y)

Read the page as a compact `{role, name, selector}` list — cheaper than
OCR-ing a screenshot to find the "Submit" button or the "Email" field. Use the
returned selectors directly in `browser_click`/`browser_type`.

## Iframes (browser_iframe)

CAPTCHAs, payment forms, rich-text editors, and third-party widgets live in
iframes; their contents are invisible to `browser_click` until you
`action='enter'` the frame. `action='list'` to find it, `enter` to dive in, do
your work, `exit` to return to the top page.

## Canvas & pixel reading

When you need to verify a `<canvas>` actually painted (not just that the
element exists), read the pixel buffer:

```js
const c = document.querySelector('canvas');
const { data } = c.getContext('2d').getImageData(0, 0, c.width, c.height);
let nz = 0; for (let i = 3; i < data.length; i += 4) if (data[i] > 0) nz++;
return { nz, w: c.width, h: c.height };
```

Pair with a before/after sample around the action — a flat pixel count after a
stroke means the tool is a no-op.
