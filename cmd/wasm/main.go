//go:build js && wasm

package main

import (
	"fmt"
	"parrot/internal/engine"
	"syscall/js"
)

func main() {
	js.Global().Set("__engineReady", js.ValueOf(true))
	var app = engine.NewApp()
	js.Global().Set("execute", js.FuncOf(func(this js.Value, args []js.Value) (result any) {
		if len(args) < 1 || args[0].Type() != js.TypeString {
			return map[string]any{"stdout": "", "stderr": "execute: expected a string",
				"exitCode": 2, "cwd": app.Cwd(), "user": app.User()}
		}
		defer func() {
			if r := recover(); r != nil {
				result = map[string]any{
					"stdout": "", "stderr": fmt.Sprintf("internal error: %v", r),
					"exitCode": 2, "cwd": app.Cwd(), "user": app.User(),
				}
			}
		}()
		r := app.Execute(args[0].String())
		return map[string]any{
			"stdout":   r.Stdout,
			"stderr":   r.Stderr,
			"exitCode": r.ExitCode,
			"cwd":      r.Cwd,
			"user":     r.User,
		}
	}))

	js.Global().Set("complete", js.FuncOf(func(this js.Value, args []js.Value) (result any) {
		if len(args) < 1 || args[0].Type() != js.TypeString {
			return []any{}
		}
		defer func() {
			if recover() != nil {
				result = []any{}
			}
		}()
		matches := app.Complete(args[0].String())
		out := make([]any, len(matches))
		for i, m := range matches {
			out[i] = m
		}
		return out
	}))

	js.Global().Set("upload", js.FuncOf(func(this js.Value, args []js.Value) (result any) {
		if len(args) < 2 || args[0].Type() != js.TypeString {
			return map[string]any{"stdout": "", "stderr": "upload: expected a path and bytes",
				"exitCode": 2, "cwd": app.Cwd(), "user": app.User()}
		}
		defer func() {
			if r := recover(); r != nil {
				result = map[string]any{"stdout": "", "stderr": fmt.Sprintf("internal error: %v", r),
					"exitCode": 2, "cwd": app.Cwd(), "user": app.User()}
			}
		}()
		data := make([]byte, args[1].Get("length").Int())
		js.CopyBytesToGo(data, args[1])
		r := app.Upload(args[0].String(), data)
		return map[string]any{"stdout": r.Stdout, "stderr": r.Stderr, "exitCode": r.ExitCode,
			"cwd": r.Cwd, "user": r.User}
	}))

	js.Global().Set("snapshot", js.FuncOf(func(this js.Value, args []js.Value) (result any) {
		defer func() {
			if recover() != nil {
				result = js.Null()
			}
		}()
		data, err := app.Snapshot()
		if err != nil {
			return js.Null()
		}
		out := js.Global().Get("Uint8Array").New(len(data))
		js.CopyBytesToJS(out, data)
		return out
	}))

	js.Global().Set("loadSession", js.FuncOf(func(this js.Value, args []js.Value) (result any) {
		if len(args) < 1 {
			return false
		}
		defer func() {
			if recover() != nil {
				result = false
			}
		}()
		data := make([]byte, args[0].Get("length").Int())
		js.CopyBytesToGo(data, args[0])
		if err := app.Restore(data); err != nil {
			return false
		}
		return true
	}))

	select {}
}
