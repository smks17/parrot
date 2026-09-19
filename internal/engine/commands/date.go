package commands

import (
	"fmt"
	"parrot/internal/engine/clock"
	"parrot/internal/engine/user"
	"strconv"
	"strings"
	"time"
)

type Date struct{}

var _ Command = Date{}

func (date Date) Name() string {
	return "date"
}

func (date Date) Usage() string {
	return "date [-u] [-s VALUE] [+FORMAT]  — print or set the date and time"
}

const defaultLayout = "Mon Jan _2 2:03:04 MST 2002"

func (date Date) Run(ctx *Context, args []string) int {
	var utc, setting bool
	var format, value string

	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "-u" || arg == "--utc" || arg == "--universal":
			utc = true
		case arg == "-s" || arg == "--set":
			if i+1 >= len(args) {
				fmt.Fprintf(ctx.Stderr, "%s: option requires an argument -- '%s'\n", date.Name(), arg)
				return 1
			}
			i++
			setting, value = true, args[i]
		case strings.HasPrefix(arg, "-s="), strings.HasPrefix(arg, "--set="):
			setting, value = true, arg[strings.Index(arg, "=")+1:]
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

	shown := now(ctx)
	if setting {
		if ctx.User.Name != user.RootName {
			fmt.Fprintf(ctx.Stderr, "%s: cannot set date: Operation not permitted\n", date.Name())
			return 1
		}
		set, err := parseDate(value, shown)
		if err != nil {
			fmt.Fprintf(ctx.Stderr, "%s: invalid date '%s'\n", date.Name(), value)
			return 1
		}
		clock.Set(set)
		shown = set
	}
	if utc {
		shown = shown.UTC()
	}

	if format == "" {
		fmt.Fprintln(ctx.Stdout, shown.Format(defaultLayout))
		return 0
	}
	fmt.Fprintln(ctx.Stdout, strftime(format, shown))
	return 0
}

var setLayouts = []string{
	"2006-01-02 15:04:05",
	"2006-01-02T15:04:05",
	"2006-01-02 15:04",
	"2006-01-02",
	time.RFC3339,
	"15:04:05",
	"15:04",
}

func parseDate(value string, ref time.Time) (time.Time, error) {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "@") {
		secs, err := strconv.ParseInt(value[1:], 10, 64)
		if err != nil {
			return time.Time{}, err
		}
		return time.Unix(secs, 0).In(ref.Location()), nil
	}

	for _, layout := range setLayouts {
		t, err := time.ParseInLocation(layout, value, ref.Location())
		if err != nil {
			continue
		}
		if t.Year() == 0 { // a time-only layout: keep today
			y, m, d := ref.Date()
			t = time.Date(y, m, d, t.Hour(), t.Minute(), t.Second(), 0, ref.Location())
		}
		return t, nil
	}
	return time.Time{}, fmt.Errorf("invalid date %q", value)
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
