export interface ExecResult {
  stdout: string;
  stderr: string;
  exitCode: number;
  cwd: string;
  user: string;
}

declare global {
  interface Window {
    execute?: (input: string) => ExecResult;
    complete?: (prefix: string) => string[];
    upload?: (path: string, data: Uint8Array) => ExecResult;
    snapshot?: () => Uint8Array | null;
    loadSession?: (data: Uint8Array) => boolean;
    __engineReady?: boolean;
  }
}

declare class Go {
  importObject: WebAssembly.Imports;
  run(instance: WebAssembly.Instance): Promise<void>;
}

let readyPromise: Promise<void> | null = null;

export function loadEngine(): Promise<void> {
  if (readyPromise) return readyPromise;

  readyPromise = new Promise((resolve, reject) => {
    const script = document.createElement("script");
    script.src = "/wasm_exec.js";
    script.onload = () => {
      const go = new Go();
      WebAssembly.instantiateStreaming(fetch("/engine.wasm"), go.importObject)
        .then((result) => {
          void go.run(result.instance);
          const deadline = Date.now() + 5000;
          const check = () => {
            if (window.__engineReady) {
              resolve();
              return;
            }
            if (Date.now() > deadline) {
              reject(new Error("engine loaded but execute was not registered"));
              return;
            }
            setTimeout(check, 10);
          };
          check();
        })
        .catch(reject);
    };
    script.onerror = () => reject(new Error("failed to load wasm_exec.js"));
    document.body.appendChild(script);
  });

  return readyPromise;
}

export function execute(input: string): ExecResult {
  if (!window.execute) {
    throw new Error("execute() called before the engine finished loading");
  }
  return window.execute(input);
}

export function complete(prefix: string): string[] {
  if (!window.complete) {
    throw new Error("complete() called before the engine finished loading");
  }
  return window.complete(prefix);
}

export function upload(path: string, data: Uint8Array): ExecResult {
  if (!window.upload) {
    throw new Error("upload() called before the engine finished loading");
  }
  return window.upload(path, data);
}

export function snapshot(): Uint8Array | null {
  if (!window.snapshot) {
    throw new Error("snapshot() called before the engine finished loading");
  }
  return window.snapshot();
}

export function loadSession(data: Uint8Array): boolean {
  if (!window.loadSession) {
    throw new Error("loadSession() called before the engine finished loading");
  }
  return window.loadSession(data);
}
