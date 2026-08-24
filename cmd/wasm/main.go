//go:build js && wasm

package main

import (
	"fmt"
	"parrot/internal/engine"
	"syscall/js"
)

func main() {
	js.Global().Set("__engineReady", js.ValueOf(true))
	var session = engine.NewSession()
	js.Global().Set("execute", js.FuncOf(func(this js.Value, args []js.Value) (result any) {
		if len(args) < 1 || args[0].Type() != js.TypeString {
			return map[string]any{"stdout": "", "stderr": "execute: expected a string",
				"exitCode": 2, "cwd": session.Cwd()}
		}
		defer func() {
			if r := recover(); r != nil {
				result = map[string]any{
					"stdout": "", "stderr": fmt.Sprintf("internal error: %v", r),
					"exitCode": 2, "cwd": session.Cwd(),
				}
			}
		}()
		r := session.Execute(args[0].String())
		return map[string]any{
			"stdout":   r.Stdout,
			"stderr":   r.Stderr,
			"exitCode": r.ExitCode,
			"cwd":      r.Cwd,
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
		matches := session.Complete(args[0].String())
		out := make([]any, len(matches))
		for i, m := range matches {
			out[i] = m
		}
		return out
	}))

	js.Global().Set("upload", js.FuncOf(func(this js.Value, args []js.Value) (result any) {
		if len(args) < 2 || args[0].Type() != js.TypeString {
			return map[string]any{"stdout": "", "stderr": "upload: expected a path and bytes",
				"exitCode": 2, "cwd": session.Cwd()}
		}
		defer func() {
			if r := recover(); r != nil {
				result = map[string]any{"stdout": "", "stderr": fmt.Sprintf("internal error: %v", r),
					"exitCode": 2, "cwd": session.Cwd()}
			}
		}()
		data := make([]byte, args[1].Get("length").Int())
		js.CopyBytesToGo(data, args[1])
		r := session.Upload(args[0].String(), data)
		return map[string]any{"stdout": r.Stdout, "stderr": r.Stderr, "exitCode": r.ExitCode, "cwd": r.Cwd}
	}))

	select {}
}
