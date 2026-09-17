package commands

import (
	"bufio"
	"fmt"
	"strconv"
	"strings"
)

type Cut struct{}

var _ Command = Cut{}

func (c Cut) Name() string { return "cut" }

func (c Cut) Usage() string {
	return "cut [-d delim] -f n[,n...] [file...]  — print selected fields of each line"
}

func (c Cut) Run(ctx *Context, args []string) int {
	delim, list := "\t", ""
	var files []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "-d" || arg == "-f":
			if i+1 >= len(args) {
				fmt.Fprintf(ctx.Stderr, "%s: option requires an argument -- '%s'\n", c.Name(), arg[1:])
				return 2
			}
			i++
			if arg == "-d" {
				delim = args[i]
			} else {
				list = args[i]
			}
		case strings.HasPrefix(arg, "-d"):
			delim = arg[2:]
		case strings.HasPrefix(arg, "-f"):
			list = arg[2:]
		default:
			files = append(files, arg)
		}
	}
	if len(delim) != 1 {
		fmt.Fprintf(ctx.Stderr, "%s: the delimiter must be a single character\n", c.Name())
		return 2
	}
	if list == "" {
		fmt.Fprintf(ctx.Stderr, "%s: you must specify a list of fields\n", c.Name())
		return 2
	}
	var fields []int
	for _, item := range strings.Split(list, ",") {
		n, err := strconv.Atoi(item)
		if err != nil || n < 1 {
			fmt.Fprintf(ctx.Stderr, "%s: invalid field value '%s'\n", c.Name(), item)
			return 2
		}
		fields = append(fields, n)
	}

	in, exit := input(ctx, c.Name(), files)
	if exit != 0 {
		return exit
	}
	scanner := bufio.NewScanner(in)
	for scanner.Scan() {
		line := scanner.Text()
		// Real cut prints a line with no delimiter unchanged.
		if !strings.Contains(line, delim) {
			fmt.Fprintln(ctx.Stdout, line)
			continue
		}
		parts := strings.Split(line, delim)
		var picked []string
		for _, n := range fields {
			if n <= len(parts) {
				picked = append(picked, parts[n-1])
			}
		}
		fmt.Fprintln(ctx.Stdout, strings.Join(picked, delim))
	}
	return 0
}

func init() { Register(Cut{}) }
