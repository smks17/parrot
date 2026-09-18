package commands

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

type Date struct{}

var _ Command = Date{}

func (date Date) Name() string {
	return "date"
}

func (date Date) Usage() string { return "date [-u] [+FORMAT]  — print the current date and time" }

const defaultLayout = "Mon Jan _2 2:03:04 MST 2002"

func (date Date) Run(ctx *Context, args []string) int {
	var utc bool
	var format string

	for _, arg := range args {
		switch {
		case arg == "-u" || arg == "--utc" || arg == "--universal":
			utc = true
		case strings.HasPrefix(arg, "+"):
			if format != "" {
				fmt.Fprintf(ctx.Stderr, "%s: extra operand '%s'\n", date.Name(), arg)
				return 1
			}
			format = arg[1:]
		default:
			fmt.Fprintf(ctx.Stderr, "%s: invalid option -- '%s'\n", date.Name(), arg)
			return 1
		}
	}

	now := time.Now()
	if utc {
		now = now.UTC()
	}

	if format == "" {
		fmt.Fprintln(ctx.Stdout, now.Format(defaultLayout))
		return 0
	}
	fmt.Fprintln(ctx.Stdout, strftime(format, now))
	return 0
}

func strftime(format string, t time.Time) string {
	var b strings.Builder
	runes := []rune(format)

	for i := 0; i < len(runes); i++ {
		if runes[i] != '%' || i+1 >= len(runes) {
			b.WriteRune(runes[i])
			continue
		}

		i++
		switch runes[i] {
		case 'Y':
			b.WriteString(t.Format("2006"))
		case 'y':
			b.WriteString(t.Format("06"))
		case 'm':
			b.WriteString(t.Format("01"))
		case 'd':
			b.WriteString(t.Format("02"))
		case 'e':
			b.WriteString(t.Format("_2"))
		case 'H':
			b.WriteString(t.Format("15"))
		case 'I':
			b.WriteString(t.Format("03"))
		case 'M':
			b.WriteString(t.Format("04"))
		case 'S':
			b.WriteString(t.Format("05"))
		case 'p':
			b.WriteString(t.Format("PM"))
		case 'P':
			b.WriteString(t.Format("pm"))
		case 'a':
			b.WriteString(t.Format("Mon"))
		case 'A':
			b.WriteString(t.Format("Monday"))
		case 'b', 'h':
			b.WriteString(t.Format("Jan"))
		case 'B':
			b.WriteString(t.Format("January"))
		case 'j':
			b.WriteString(fmt.Sprintf("%03d", t.YearDay()))
		case 'Z':
			b.WriteString(t.Format("MST"))
		case 'z':
			b.WriteString(t.Format("-0700"))
		case 's':
			b.WriteString(strconv.FormatInt(t.Unix(), 10))
		case 'N':
			b.WriteString(fmt.Sprintf("%09d", t.Nanosecond()))
		case 'F':
			b.WriteString(t.Format("2006-01-02"))
		case 'T':
			b.WriteString(t.Format("15:04:05"))
		case 'D':
			b.WriteString(t.Format("01/02/06"))
		case 'R':
			b.WriteString(t.Format("15:04"))
		case 'c':
			b.WriteString(t.Format(defaultLayout))
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		case '%':
			b.WriteByte('%')
		default:
			b.WriteByte('%')
			b.WriteRune(runes[i])
		}
	}

	return b.String()
}

func init() { Register(Date{}) }
