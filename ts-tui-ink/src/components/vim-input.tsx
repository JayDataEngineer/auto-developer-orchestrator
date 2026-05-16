import React, { useCallback, useEffect, useReducer, useRef, useState } from "react";
import { Box, Text, useFocus, useInput } from "ink";
import { useAui, useAuiState } from "@assistant-ui/react-ink";

type VimMode = "insert" | "normal";

type BufferState = {
  text: string;
  cursorOffset: number;
  preferredColumn: number | undefined;
};

type Action =
  | { type: "insert"; text: string }
  | { type: "delete-backward" }
  | { type: "delete-forward" }
  | { type: "move-left" }
  | { type: "move-right" }
  | { type: "move-up" }
  | { type: "move-down" }
  | { type: "move-home"; multiLine: boolean }
  | { type: "move-end"; multiLine: boolean }
  | { type: "move-word-left" }
  | { type: "move-word-right" }
  | { type: "kill-word-backward" }
  | { type: "kill-word-forward" }
  | { type: "kill-start"; multiLine: boolean }
  | { type: "kill-end"; multiLine: boolean }
  | { type: "set-text"; text: string }
  | { type: "set-cursor"; cursorOffset: number };

const clamp = (value: number, min: number, max: number) =>
  Math.min(Math.max(value, min), max);

const gSegmenter = new Intl.Segmenter(undefined, { granularity: "grapheme" });
const wSegmenter = new Intl.Segmenter(undefined, { granularity: "word" });

const stepLeft = (text: string, offset: number) => {
  if (offset <= 0) return 0;
  let prev = 0;
  for (const { index } of gSegmenter.segment(text)) {
    if (index >= offset) break;
    prev = index;
  }
  return prev;
};

const stepRight = (text: string, offset: number) => {
  if (offset >= text.length) return text.length;
  for (const { index, segment } of gSegmenter.segment(text)) {
    const end = index + segment.length;
    if (end > offset) return end;
  }
  return text.length;
};

const getGraphemeAt = (text: string, offset: number) => {
  if (offset >= text.length) return "";
  for (const { index, segment } of gSegmenter.segment(text)) {
    if (index === offset) return segment;
    if (index > offset) return "";
  }
  return "";
};

const getLineStart = (text: string, offset: number) => {
  if (offset === 0) return 0;
  const idx = text.lastIndexOf("\n", offset - 1);
  return idx === -1 ? 0 : idx + 1;
};

const getLineEnd = (text: string, offset: number) => {
  const idx = text.indexOf("\n", offset);
  return idx === -1 ? text.length : idx;
};

const prevWord = (text: string, offset: number) => {
  let result = 0;
  for (const seg of wSegmenter.segment(text)) {
    if (seg.index >= offset) break;
    if (seg.isWordLike) result = seg.index;
  }
  return result;
};

const nextWord = (text: string, offset: number) => {
  for (const seg of wSegmenter.segment(text)) {
    const end = seg.index + seg.segment.length;
    if (end <= offset) continue;
    if (seg.isWordLike) return end;
  }
  return text.length;
};

const moveVertical = (
  text: string,
  offset: number,
  preferred: number | undefined,
  dir: -1 | 1,
) => {
  const start = getLineStart(text, offset);
  const end = getLineEnd(text, offset);
  const col = preferred ?? offset - start;
  const adjacentIdx = dir === -1 ? start - 1 : end;
  if (adjacentIdx < 0 || adjacentIdx >= text.length) {
    return { cursorOffset: offset, preferredColumn: col };
  }
  const base = dir === -1 ? adjacentIdx : adjacentIdx + 1;
  const aStart = getLineStart(text, base);
  const aEnd = getLineEnd(text, base);
  return {
    cursorOffset: clamp(aStart + col, aStart, aEnd),
    preferredColumn: col,
  };
};

const reducer = (state: BufferState, action: Action): BufferState => {
  switch (action.type) {
    case "insert": {
      if (!action.text) return state;
      const nextText =
        state.text.slice(0, state.cursorOffset) +
        action.text +
        state.text.slice(state.cursorOffset);
      return {
        text: nextText,
        cursorOffset: state.cursorOffset + action.text.length,
        preferredColumn: undefined,
      };
    }
    case "delete-backward": {
      if (state.cursorOffset === 0) return state;
      const prev = stepLeft(state.text, state.cursorOffset);
      return {
        text: state.text.slice(0, prev) + state.text.slice(state.cursorOffset),
        cursorOffset: prev,
        preferredColumn: undefined,
      };
    }
    case "delete-forward": {
      if (state.cursorOffset >= state.text.length) return state;
      const next = stepRight(state.text, state.cursorOffset);
      return {
        text: state.text.slice(0, state.cursorOffset) + state.text.slice(next),
        cursorOffset: state.cursorOffset,
        preferredColumn: undefined,
      };
    }
    case "move-left":
      return { ...state, cursorOffset: stepLeft(state.text, state.cursorOffset), preferredColumn: undefined };
    case "move-right":
      return { ...state, cursorOffset: stepRight(state.text, state.cursorOffset), preferredColumn: undefined };
    case "move-up": {
      const r = moveVertical(state.text, state.cursorOffset, state.preferredColumn, -1);
      return { ...state, ...r };
    }
    case "move-down": {
      const r = moveVertical(state.text, state.cursorOffset, state.preferredColumn, 1);
      return { ...state, ...r };
    }
    case "move-home": {
      const next = action.multiLine ? getLineStart(state.text, state.cursorOffset) : 0;
      return { ...state, cursorOffset: next, preferredColumn: undefined };
    }
    case "move-end": {
      const next = action.multiLine ? getLineEnd(state.text, state.cursorOffset) : state.text.length;
      return { ...state, cursorOffset: next, preferredColumn: undefined };
    }
    case "move-word-left":
      return { ...state, cursorOffset: prevWord(state.text, state.cursorOffset), preferredColumn: undefined };
    case "move-word-right":
      return { ...state, cursorOffset: nextWord(state.text, state.cursorOffset), preferredColumn: undefined };
    case "kill-word-backward": {
      const next = prevWord(state.text, state.cursorOffset);
      if (next === state.cursorOffset) return state;
      return {
        text: state.text.slice(0, next) + state.text.slice(state.cursorOffset),
        cursorOffset: next,
        preferredColumn: undefined,
      };
    }
    case "kill-word-forward": {
      const next = nextWord(state.text, state.cursorOffset);
      if (next === state.cursorOffset) return state;
      return {
        text: state.text.slice(0, state.cursorOffset) + state.text.slice(next),
        cursorOffset: state.cursorOffset,
        preferredColumn: undefined,
      };
    }
    case "kill-start": {
      const start = action.multiLine ? getLineStart(state.text, state.cursorOffset) : 0;
      if (start === state.cursorOffset) return state;
      return {
        text: state.text.slice(0, start) + state.text.slice(state.cursorOffset),
        cursorOffset: start,
        preferredColumn: undefined,
      };
    }
    case "kill-end": {
      const lineEnd = action.multiLine ? getLineEnd(state.text, state.cursorOffset) : state.text.length;
      const rangeEnd =
        action.multiLine && lineEnd === state.cursorOffset && lineEnd < state.text.length
          ? lineEnd + 1
          : lineEnd;
      if (rangeEnd === state.cursorOffset) return state;
      return {
        text: state.text.slice(0, state.cursorOffset) + state.text.slice(rangeEnd),
        cursorOffset: state.cursorOffset,
        preferredColumn: undefined,
      };
    }
    case "set-text":
      return { text: action.text, cursorOffset: action.text.length, preferredColumn: undefined };
    case "set-cursor":
      return { ...state, cursorOffset: clamp(action.cursorOffset, 0, state.text.length), preferredColumn: undefined };
  }
};

const PENDING_SYNC_CAP = 64;

interface VimInputProps {
  submitOnEnter?: boolean;
  placeholder?: string;
  autoFocus?: boolean;
  multiLine?: boolean;
  onSubmit?: (text: string) => void;
}

function lineForDelete(text: string, offset: number): { start: number; end: number } {
  const s = getLineStart(text, offset);
  let e = text.indexOf("\n", offset);
  if (e === -1) e = text.length;
  else e = e + 1;
  return { start: s, end: e };
}

export function VimInput({
  submitOnEnter = false,
  placeholder = "",
  autoFocus = true,
  multiLine = false,
  onSubmit,
}: VimInputProps) {
  const aui = useAui();
  const storeText = useAuiState((s) => s.composer.text);
  const { isFocused } = useFocus({ autoFocus });

  const [vimMode, setVimMode] = useState<VimMode>("normal");
  const [state, dispatch] = useReducer(reducer, { text: storeText, cursorOffset: 0, preferredColumn: undefined });
  const stateRef = useRef(state);
  stateRef.current = state;
  const pendingSyncRef = useRef(new Map<string, number>());
  const pendingKeysRef = useRef<string[]>([]);
  const killRingRef = useRef<string>("");

  useEffect(() => {
    const counter = pendingSyncRef.current;
    const pending = counter.get(storeText) ?? 0;
    if (pending > 0) {
      if (pending === 1) counter.delete(storeText);
      else counter.set(storeText, pending - 1);
      return;
    }
    if (storeText === state.text) return;
    counter.clear();
    dispatch({ type: "set-text", text: storeText });
  }, [storeText, state.text]);

  const commit = useCallback(
    (action: Action, opts?: { syncText?: boolean }) => {
      const prev = stateRef.current;
      const next = reducer(prev, action);
      dispatch(action);
      stateRef.current = next;
      if (opts?.syncText !== false && next.text !== prev.text) {
        const counter = pendingSyncRef.current;
        if (counter.size >= PENDING_SYNC_CAP) counter.clear();
        counter.set(next.text, (counter.get(next.text) ?? 0) + 1);
        aui.composer().setText(next.text);
      }
    },
    [aui],
  );

  // Direct text commit — replaces the repeated sync boilerplate in vim commands.
  const commitText = useCallback(
    (newText: string, cursorOffset: number) => {
      stateRef.current = { text: newText, cursorOffset, preferredColumn: undefined };
      dispatch({ type: "set-text", text: newText });
      dispatch({ type: "set-cursor", cursorOffset });
      const counter = pendingSyncRef.current;
      if (counter.size >= PENDING_SYNC_CAP) counter.clear();
      counter.set(newText, (counter.get(newText) ?? 0) + 1);
      aui.composer().setText(newText);
    },
    [aui],
  );

  const submit = useCallback(() => {
    const text = stateRef.current.text;
    if (onSubmit) {
      onSubmit(text);
      return;
    }
    aui.composer().send();
  }, [aui, onSubmit]);

  useInput(
    useCallback(
      (input: string, key: any) => {
        if (!isFocused) return;

        if (vimMode === "normal") {
          if (key.escape) { setVimMode("normal"); pendingKeysRef.current = []; return; }
          if (input === "i") { setVimMode("insert"); pendingKeysRef.current = []; return; }
          if (input === "a") {
            commit({ type: "move-right" }, { syncText: false });
            setVimMode("insert");
            pendingKeysRef.current = [];
            return;
          }
          if (input === "I") {
            commit({ type: "move-home", multiLine }, { syncText: false });
            setVimMode("insert");
            pendingKeysRef.current = [];
            return;
          }
          if (input === "A") {
            commit({ type: "move-end", multiLine }, { syncText: false });
            setVimMode("insert");
            pendingKeysRef.current = [];
            return;
          }
          if (input === "o") {
            const { text: t, cursorOffset: c } = stateRef.current;
            commitText(t.slice(0, c) + "\n" + t.slice(c), c + 1);
            setVimMode("insert");
            pendingKeysRef.current = [];
            return;
          }
          if (input === "O") {
            const { text: t, cursorOffset: c } = stateRef.current;
            const lineStart = getLineStart(t, c);
            commitText(t.slice(0, lineStart) + "\n" + t.slice(lineStart), lineStart);
            setVimMode("insert");
            pendingKeysRef.current = [];
            return;
          }
          if (input === "h") { commit({ type: "move-left" }, { syncText: false }); pendingKeysRef.current = []; return; }
          if (input === "l") { commit({ type: "move-right" }, { syncText: false }); pendingKeysRef.current = []; return; }
          if (input === "j") { commit({ type: "move-down" }, { syncText: false }); pendingKeysRef.current = []; return; }
          if (input === "k") { commit({ type: "move-up" }, { syncText: false }); pendingKeysRef.current = []; return; }
          if (input === "w") { commit({ type: "move-word-right" }, { syncText: false }); pendingKeysRef.current = []; return; }
          if (input === "b") { commit({ type: "move-word-left" }, { syncText: false }); pendingKeysRef.current = []; return; }
          if (input === "0") { commit({ type: "move-home", multiLine: false }, { syncText: false }); pendingKeysRef.current = []; return; }
          if (input === "^") {
            const { text: t, cursorOffset: c } = stateRef.current;
            const ls = getLineStart(t, c);
            const firstNonBlank = t.slice(ls).search(/\S/);
            const target = firstNonBlank === -1 ? ls : ls + firstNonBlank;
            stateRef.current = { ...stateRef.current, cursorOffset: target, preferredColumn: undefined };
            dispatch({ type: "set-cursor", cursorOffset: target });
            pendingKeysRef.current = [];
            return;
          }
          if (input === "$") { commit({ type: "move-end", multiLine }, { syncText: false }); pendingKeysRef.current = []; return; }
          if (input === "x") { commit({ type: "delete-forward" }); pendingKeysRef.current = []; return; }
          if (input === "X") { commit({ type: "delete-backward" }); pendingKeysRef.current = []; return; }
          if (input === "p" || input === "P") {
            if (killRingRef.current) {
              const { text: t, cursorOffset: c } = stateRef.current;
              const newText = t.slice(0, c) + killRingRef.current + t.slice(c);
              const nextCursor = input === "p" ? c + killRingRef.current.length : c;
              commitText(newText, nextCursor);
            }
            pendingKeysRef.current = [];
            return;
          }
          if (input === "D" || input === "C") {
            const { text: t, cursorOffset: c } = stateRef.current;
            const lineEnd = getLineEnd(t, c);
            if (lineEnd > c) {
              killRingRef.current = t.slice(c, lineEnd);
              commitText(t.slice(0, c) + t.slice(lineEnd), c);
            }
            if (input === "C") setVimMode("insert");
            pendingKeysRef.current = [];
            return;
          }
          if (input === "d") {
            pendingKeysRef.current.push("d");
            if (pendingKeysRef.current.length >= 2 && pendingKeysRef.current.every((k) => k === "d")) {
              const { text: t, cursorOffset: c } = stateRef.current;
              const { start, end } = lineForDelete(t, c);
              killRingRef.current = t.slice(start, end);
              const newText = t.slice(0, start) + t.slice(end);
              commitText(newText, Math.min(start, newText.length));
              pendingKeysRef.current = [];
            }
            return;
          }
          // No undo stack — 'u' is intentionally unbound (vim convention)
          if (key.return && submitOnEnter) {
            submit();
            pendingKeysRef.current = [];
            return;
          }
          pendingKeysRef.current = [];
          return;
        }

        if (key.escape) {
          setVimMode("normal");
          pendingKeysRef.current = [];
          return;
        }

        const lowerInput = input.toLowerCase();

        if (key.ctrl) {
          if (lowerInput === "j") {
            if (multiLine) commit({ type: "insert", text: "\n" });
            return;
          }
          if (lowerInput === "a") { commit({ type: "move-home", multiLine }, { syncText: false }); return; }
          if (lowerInput === "e") { commit({ type: "move-end", multiLine }, { syncText: false }); return; }
          if (lowerInput === "w") { commit({ type: "kill-word-backward" }); return; }
          if (lowerInput === "u") { commit({ type: "kill-start", multiLine }); return; }
          if (lowerInput === "k") { commit({ type: "kill-end", multiLine }); return; }
          if (lowerInput === "d") { commit({ type: "delete-forward" }); return; }
          return;
        }

        if (key.meta) {
          if (lowerInput === "b") { commit({ type: "move-word-left" }, { syncText: false }); return; }
          if (lowerInput === "f") { commit({ type: "move-word-right" }, { syncText: false }); return; }
          if (lowerInput === "d") { commit({ type: "kill-word-forward" }); return; }
        }

        if (key.return) {
          const shouldInsertNewline = multiLine && (!submitOnEnter || key.shift);
          if (shouldInsertNewline) { commit({ type: "insert", text: "\n" }); return; }
          if (submitOnEnter) submit();
          return;
        }

        if (key.leftArrow) { commit({ type: "move-left" }, { syncText: false }); return; }
        if (key.rightArrow) { commit({ type: "move-right" }, { syncText: false }); return; }
        if (multiLine && key.upArrow) { commit({ type: "move-up" }, { syncText: false }); return; }
        if (multiLine && key.downArrow) { commit({ type: "move-down" }, { syncText: false }); return; }
        if (key.home) { commit({ type: "move-home", multiLine }, { syncText: false }); return; }
        if (key.end) { commit({ type: "move-end", multiLine }, { syncText: false }); return; }
        if (key.backspace) { commit({ type: "delete-backward" }); return; }
        if (key.delete) { commit({ type: "delete-forward" }); return; }

        if (input && !key.ctrl && !key.meta) {
          commit({ type: "insert", text: input });
        }
      },
      [isFocused, vimMode, commit, commitText, multiLine, submitOnEnter, submit],
    ),
    { isActive: isFocused },
  );

  const { text, cursorOffset } = state;
  const hasText = text.length > 0;
  const isShowingPlaceholder = !hasText && placeholder.length > 0;
  const before = hasText ? text.slice(0, cursorOffset) : "";
  const charAtCursor = hasText ? getGraphemeAt(text, cursorOffset) : "";
  const isOnNewline = charAtCursor === "\n";
  const atCursor = charAtCursor === "" || isOnNewline ? " " : charAtCursor;
  const after = hasText
    ? isOnNewline
      ? text.slice(cursorOffset)
      : text.slice(cursorOffset + charAtCursor.length)
    : placeholder;

  return (
    <Box flexGrow={1}>
      {!isFocused ? (
        <Text dimColor={isShowingPlaceholder}>
          {hasText ? text : placeholder}
        </Text>
      ) : (
        <Box flexGrow={1}>
          <Text
            dimColor={isShowingPlaceholder}
            bold={vimMode === "normal"}
            color={vimMode === "normal" ? "yellow" : undefined}
          >
            {before}
            <Text inverse>{atCursor}</Text>
            {after}
          </Text>
          {vimMode === "normal" && (
            <Text color="yellow" dimColor> -- NORMAL --</Text>
          )}
        </Box>
      )}
    </Box>
  );
}
