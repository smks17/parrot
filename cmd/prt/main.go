package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
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
	return report(app.RunScript(string(src), args))
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

		result := app.Execute(scanner.Text())
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

func report(result engine.Result) int {
	fmt.Fprint(os.Stdout, result.Stdout)
	fmt.Fprint(os.Stderr, result.Stderr)
	return result.ExitCode
}
