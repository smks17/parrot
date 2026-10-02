package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"

	"parrot/internal/engine"
)

// interruptible runs one piece of input with Ctrl+C wired to cancel it.
func interruptible(run func(ctx context.Context) engine.Result) engine.Result {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	defer signal.Stop(sig)

	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-sig:
			cancel()
		case <-done:
		}
	}()

	return run(ctx)
}

// report echoes what a terminal shows for an interrupted command
func report(result engine.Result) int {
	if result.Interrupted {
		fmt.Fprintln(os.Stdout, "^C")
	}
	return result.ExitCode
}

func login(app *engine.App, scanner *bufio.Scanner) bool {
	for {
		fmt.Printf("%s login: ", engine.ShellName)
		if !scanner.Scan() {
			fmt.Println()
			return false
		}
		name := strings.TrimSpace(scanner.Text())
		if name == "" {
			continue
		}
		fmt.Print("Password: ")
		if !scanner.Scan() {
			fmt.Println()
			return false
		}
		if err := app.Login(name, scanner.Text()); err != nil {
			fmt.Fprintln(os.Stderr, "Login incorrect")
			continue
		}
		return true
	}
}

func main() {
	app := engine.NewApp()

	// With a file named on the command line, run it instead of prompting.
	if len(os.Args) > 1 {
		src, err := os.ReadFile(os.Args[1])
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", engine.ShellName, err)
			os.Exit(127)
		}
		os.Exit(report(interruptible(func(ctx context.Context) engine.Result {
			return app.RunScriptStream(ctx, string(src), os.Args[2:], os.Stdout, os.Stderr)
		})))
	}
	scanner := bufio.NewScanner(os.Stdin)

	if !login(app, scanner) {
		os.Exit(1)
	}

	for {
		// root gets "#", like a real shell — su has to be visible somewhere.
		sigil := "$"
		if app.User() == "root" {
			sigil = "#"
		}
		fmt.Printf("%s%s ", app.User(), sigil)

		// Scan reports false for both EOF and read errors; the error itself
		// is checked after the loop.
		if !scanner.Scan() {
			fmt.Println()
			break
		}

		line := scanner.Text()
		result := interruptible(func(ctx context.Context) engine.Result {
			return app.ExecuteStream(ctx, line, os.Stdout, os.Stderr)
		})
		status := report(result)
		if result.Exited {
			os.Exit(status)
		}
	}

	if err := scanner.Err(); err != nil {
		fmt.Fprintln(os.Stderr, engine.ShellName+": read error:", err)
		os.Exit(1)
	}
}
