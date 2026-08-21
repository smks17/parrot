package main

import (
	"bufio"
	"fmt"
	"os"

	"parrot/internal/engine"
)

func main() {
	scanner := bufio.NewScanner(os.Stdin)

	var session = engine.NewSession()

	for {
		fmt.Print("$ ")

		// Scan reports false for both EOF and read errors; the error itself
		// is checked after the loop.
		if !scanner.Scan() {
			fmt.Println()
			break
		}

		line := scanner.Text()

		if line == "exit" {
			break
		}

		res := session.Execute(line)

		// Stdout and Stderr go to the real streams so redirection
		if res.Stdout != "" {
			fmt.Fprint(os.Stdout, res.Stdout)
		}
		if res.Stderr != "" {
			fmt.Fprint(os.Stderr, res.Stderr)
		}
	}

	if err := scanner.Err(); err != nil {
		fmt.Fprintln(os.Stderr, engine.ShellName+": read error:", err)
		os.Exit(1)
	}
}
