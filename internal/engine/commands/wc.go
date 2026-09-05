package commands

import (
	"fmt"
	"io"
	"strings"
)

type WC struct{}

type CountWC struct {
	lines *int
	words *int
	bytes *int
	chars *int
}

func (c *CountWC) print(filename string, stdout io.Writer, stderr io.Writer) int {
	var result strings.Builder
	if c.lines != nil {
		fmt.Fprintf(&result, "%d ", *c.lines)
	}
	if c.words != nil {
		fmt.Fprintf(&result, "%d ", *c.words)
	}
	if c.bytes != nil {
		fmt.Fprintf(&result, "%d ", *c.bytes)
	}
	if c.chars != nil {
		fmt.Fprintf(&result, "%d ", *c.chars)
	}
	fmt.Fprintf(&result, "%s\n", filename)
	if _, err := fmt.Fprint(stdout, result.String()); err != nil {
		fmt.Fprintf(stderr, "wc: %v\n", err)
		return 1
	}

	return 0
}

var _ Command = WC{}

func (wc WC) Name() string {
	return "wc"
}

func (wc WC) Usage() string {
	return "wc file...  — print newline, word, and byte counts for each file"
}

const (
	NONE  = 0
	LINE  = 1 << iota // 1 (0001)
	BYTES             // 2 (0010)
	CHAR              // 4 (0100)
	WORD              // 8 (1000)
	ALL   = LINE | WORD | BYTES
)

func (wc WC) Run(ctx *Context, args []string) int {
	mode := NONE
	var operands []string
	for _, arg := range args {
		if arg == "-m" || arg == "--chars" {
			mode |= CHAR
		} else if arg == "-c" || arg == "--bytes" {
			mode |= BYTES
		} else if arg == "-w" || arg == "--words" {
			mode |= WORD
		} else if arg == "-l" || arg == "--lines" {
			mode |= LINE
		} else {
			operands = append(operands, arg)
		}
	}
	if mode == NONE {
		mode = ALL
	}

	var file string
	if len(operands) > 0 {
		file = operands[0]
	} else {
		if ctx.Stdin == nil {
			fmt.Fprintf(ctx.Stderr, "%s: missing operand\n", wc.Name())
			return 1
		}
		if _, err := io.Copy(ctx.Stdout, ctx.Stdin); err != nil {
			fmt.Fprintf(ctx.Stderr, "%s: %v\n", wc.Name(), err)
			return 1
		}
		return 0
	}

	content, err := ctx.VFS.Read(file)
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "%s: %s: %v\n", wc.Name(), file, err)
		return 1
	}
	var count CountWC
	if mode&LINE == LINE || mode == ALL {
		lines := strings.Count(string(content), "\n")
		count.lines = &lines
	}
	if mode&CHAR == CHAR || mode == ALL {
		chars := len(string(content))
		count.bytes = &chars
	}
	if mode&BYTES == BYTES || mode == ALL {
		bytes := len(content)
		count.bytes = &bytes
	}
	if mode&WORD == WORD || mode == ALL {
		words := strings.Count(string(content), " ")
		count.words = &words
	}
	return count.print(file, ctx.Stdout, ctx.Stderr)
}

func init() { Register(WC{}) }
