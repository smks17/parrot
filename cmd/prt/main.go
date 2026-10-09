package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"

	"parrot/internal/engine"
	"parrot/internal/engine/osfs"
)

func main() {
	onDisk := flag.Bool("real", false, "work on the real filesystem instead of the in-memory one")
	flag.Usage = usage
	flag.Parse()

	app, err := start(*onDisk)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", engine.ShellName, err)
		os.Exit(1)
	}
	app.SetBackgroundOutput(os.Stdout, os.Stderr)

	// A named file is run as a script, with the rest of the arguments as $1
	if args := flag.Args(); len(args) > 0 {
		os.Exit(runFile(app, args[0], args[1:]))
	}
	os.Exit(repl(app, *onDisk))
}

func usage() {
	fmt.Fprintf(os.Stderr, "usage: %s [-real] [script [args...]]\n\n", engine.ShellName)
	flag.PrintDefaults()
}

// start builds the shell over whichever filesystem was asked for.
func start(onDisk bool) (*engine.App, error) {
	if !onDisk {
		return engine.NewApp(), nil
	}

	filesystem, err := osfs.New()
	if err != nil {
		return nil, err
	}

	app := engine.NewAppOn(filesystem)
	return app, nil
}

func runFile(app *engine.App, path string, args []string) int {
	src, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", engine.ShellName, err)
		return 127
	}
	return report(interruptible(app, func(ctx context.Context) engine.Result {
		return app.RunScriptStream(ctx, string(src), args, os.Stdout, os.Stderr)
	}))
}

func repl(app *engine.App, onDisk bool) int {
	scanner := bufio.NewScanner(os.Stdin)
	status := 0

	// On the real filesystem there is nobody to log in as: the shell already
	// runs as the account that started it, and the machine's own accounts are
	// not the in-memory world's.
	if !onDisk && !login(app, scanner) {
		return 1
	}

	for {
		sigil := "$"
		if app.User() == "root" {
			sigil = "#"
		}
		fmt.Printf("%s%s ", app.User(), sigil)
		// Scan reports false for both the end of input and a read error; the
		// error itself is checked after the loop.
		if !scanner.Scan() {
			fmt.Println()
			break
		}

		line := scanner.Text()
		result := interruptible(app, func(ctx context.Context) engine.Result {
			return app.ExecuteStream(ctx, line, os.Stdout, os.Stderr)
		})
		status = report(result)
		if result.Exited {
			return status
		}
	}

	if err := scanner.Err(); err != nil {
		fmt.Fprintln(os.Stderr, engine.ShellName+": read error:", err)
		return 1
	}
	return status
}

// interruptible runs one piece of input with Ctrl+C wired to cancel it, and
// Ctrl+Z, where the host has it, to stop it.
func interruptible(app *engine.App, run func(ctx context.Context) engine.Result) engine.Result {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	defer signal.Stop(sig)

	suspend := make(chan os.Signal, 1)
	if len(suspendSignals) > 0 {
		signal.Notify(suspend, suspendSignals...)
		defer signal.Stop(suspend)
	}

	done := make(chan struct{})
	defer close(done)
	go func() {
		for {
			select {
			case <-sig:
				cancel()
				return
			case <-suspend:
				app.Suspend() // the command stops; the line goes on without it
			case <-done:
				return
			}
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
