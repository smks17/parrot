package clock

import (
	"sync/atomic"
	"time"

	// The browser has no /usr/share/zoneinfo, so carry the IANA database in
	// the binary: it is what makes TZ=Asia/Tehran work at all. Importing it
	// here rather than in cmd/wasm keeps tests on the same zone data.
	_ "time/tzdata"
)

var (
	offset atomic.Int64                  // virtual now − real now, in nanoseconds
	local  atomic.Pointer[time.Location] // the zone used when TZ is unset; nil means time.Local
)

// Now is the time the shell believes it is.
func Now() time.Time { return time.Now().Add(Offset()) }

// Set moves the clock to t. It keeps ticking from there, like `date -s` does.
func Set(t time.Time) { SetOffset(time.Until(t)) }

func Offset() time.Duration { return time.Duration(offset.Load()) }

func SetOffset(d time.Duration) { offset.Store(int64(d)) }

// Zone resolves a TZ value. An unknown zone falls back to UTC, same as libc.
func Zone(tz string) *time.Location {
	if tz == "" {
		return localZone()
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return time.UTC
	}
	return loc
}

// In is Now in the zone TZ names.
func In(tz string) time.Time { return Now().In(Zone(tz)) }

// Local reports the zone in use when TZ is unset, as a name and its offset in
// seconds east of UTC — the pair a snapshot stores.
func Local() (string, int) { return Now().In(localZone()).Zone() }

func localZone() *time.Location {
	if loc := local.Load(); loc != nil {
		return loc
	}
	return time.Local
}

// SetLocal restores that zone. A snapshot is read on someone else's machine,
// so the saved session keeps the zone it was written in, not the reader's.
// An offset is enough when the name is not one the zone database knows — in
// the browser, time.Local is a bare "UTC+3:30" to begin with.
func SetLocal(name string, offset int) {
	if loc, err := time.LoadLocation(name); err == nil {
		local.Store(loc)
		return
	}
	local.Store(time.FixedZone(name, offset))
}
