package main

import (
	"bufio"
	"fmt"
	"os"

	"parrot/internal/engine"
)

func report(result engine.Result) int {
	fmt.Fprint(os.Stdout, result.Stdout)
	fmt.Fprint(os.Stderr, result.Stderr)
	return result.ExitCode
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
		os.Exit(report(app.RunScript(string(src), os.Args[2:])))
	}
	scanner := bufio.NewScanner(os.Stdin)

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

		result := app.Execute(scanner.Text())
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
