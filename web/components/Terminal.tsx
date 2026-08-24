"use client";

import { useEffect, useRef, useState } from "react";
import { complete, execute, loadEngine, upload } from "@/lib/wasm-bridge";
import styles from "./Terminal.module.css";

interface Entry {
  cwd: string;
  line: string;
  stdout: string;
  stderr: string;
}

export default function Terminal() {
  const [ready, setReady] = useState(false)
  const [loadError, setLoadError] = useState<string | null>(null)
  const [entries, setEntries] = useState<Entry[]>([])
  const [input, setInput] = useState("")
  const [cwd, setCwd] = useState("/home/friend")

  const [history, setHistory] = useState<string[]>([]);
  const [historyIndex, setHistoryIndex] = useState<number | null>(null);

  const [completions, setCompletions] = useState<string[]>([]);

  const [dragActive, setDragActive] = useState(false);

  const inputRef = useRef<HTMLInputElement>(null);
  const scrollRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    loadEngine()
      .then(() => setReady(true))
      .catch((err: Error) => setLoadError(err.message))
  }, [])

  useEffect(() => {
    if (scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight
    }
  }, [entries])

  function runLine(line: string) {
    const res = execute(line);
    setEntries((prev) => [...prev, { cwd, line, stdout: res.stdout, stderr: res.stderr }]);
    setCwd(res.cwd);
    setHistory((prev) => [...prev, line]);
    setHistoryIndex(null);
  }

  function moveHistory(step: number) {
    if (history.length === 0) return;
    const current = historyIndex === null ? history.length : historyIndex;
    const next = current + step;

    if (next < 0 || next >= history.length) {
      setHistoryIndex(null);
      setInput("");
      return;
    }
    setHistoryIndex(next);
    setInput(history[next]);
  }

  function handleTab() {
    setCompletions([]);

    // Complete only the last word — "cat dem<Tab>" completes "dem".
    const lastSpace = input.lastIndexOf(" ");
    const prefix = input.slice(lastSpace + 1);
    const before = input.slice(0, lastSpace + 1); // "" if there was no space

    const matches = complete(prefix);
    if (matches.length === 0) return;
    if (matches.length === 1) {
      setInput(before + matches[0]);
      return;
    }
    setCompletions(matches);
  }

  function handleKeyDown(event: React.KeyboardEvent<HTMLInputElement>) {
    if (event.key === "Tab") {
      event.preventDefault();
      handleTab();
      return;
    }

    if (event.key === "Enter") {
      runLine(input);
      setInput("");
      setCompletions([]);
      return;
    }

    if (event.key === "ArrowUp") {
      event.preventDefault();
      moveHistory(-1);
      return;
    }

    if (event.key === "ArrowDown") {
      event.preventDefault();
      moveHistory(1);
    }
  }

  function focusInput() {
    // A click that ends a text-drag-select still fires as a click; refocusing
    // unconditionally would collapse the selection the user just made.
    if (window.getSelection()?.toString()) return;
    inputRef.current?.focus();
  }

  function handleDragOver(event: React.DragEvent<HTMLDivElement>) {
    event.preventDefault();
    setDragActive(true);
  }

  function handleDragLeave(event: React.DragEvent<HTMLDivElement>) {
    event.preventDefault();
    setDragActive(false);
  }

  async function handleDrop(event: React.DragEvent<HTMLDivElement>) {
    event.preventDefault();
    setDragActive(false);

    const files = Array.from(event.dataTransfer.files);
    const results = await Promise.all(
      files.map(async (file) => {
        const bytes = new Uint8Array(await file.arrayBuffer());
        return { file, res: upload("/downloads/" + file.name, bytes) };
      })
    );

    setEntries((prev) => [
      ...prev,
      ...results.map(({ file, res }) => ({
        cwd,
        line: `upload ${file.name}`,
        stdout: res.stdout,
        stderr: res.stderr,
      })),
    ]);
    if (results.length > 0) {
      setCwd(results[results.length - 1].res.cwd);
    }
  }

  if (loadError) {
    return <p className={styles.error}>failed to load engine: {loadError}</p>;
  }

  return (
    <div
      className={styles.terminal}
      onClick={focusInput}
      onDragOver={handleDragOver}
      onDragLeave={handleDragLeave}
      onDrop={handleDrop}
    >
      {dragActive && (
        <div className={styles.dropOverlay} aria-hidden="true">
          drop to upload to /downloads
        </div>
      )}
      <div className={styles.scrollback} ref={scrollRef}>
        {entries.map((entry, i) => (
          <div key={i} className={styles.entry}>
            <div className={styles.promptLine}>
              <span className={styles.prompt}>{entry.cwd} $</span>{" "}
              <span>{entry.line}</span>
            </div>
            {entry.stdout && <pre className={styles.stdout}>{entry.stdout}</pre>}
            {entry.stderr && <pre className={styles.stderr}>{entry.stderr}</pre>}
          </div>
        ))}
        <div className={styles.promptLine}>
          <span className={styles.prompt}>{cwd} $</span>
          <input
            ref={inputRef}
            className={styles.input}
            value={input}
            onChange={(e) => {
              setInput(e.target.value);
              setHistoryIndex(null);
              setCompletions([]);
            }}
            onKeyDown={handleKeyDown}
            disabled={!ready}
            autoFocus
            spellCheck={false}
            autoComplete="off"
            autoCapitalize="off"
            aria-label="terminal input"
          />
        </div>
        {completions.length > 0 && (
          <div className={styles.completions}>{completions.join("  ")}</div>
        )}
      </div>
    </div>
  );
}
