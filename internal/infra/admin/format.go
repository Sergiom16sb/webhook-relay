package admin

import (
	"fmt"
	"net/url"
	"time"
)

// fmtInt formats an int for display.
func fmtInt(n int) string { return fmt.Sprintf("%d", n) }

// fmtShortTime formats a time as "2m ago", "5h ago", etc. — minimal.
func fmtShortTime(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

// fmtTime formats a time as "YYYY-MM-DD HH:MM:SS".
func fmtTime(t time.Time) string {
	return t.Format("2006-01-02 15:04:05")
}

// fmtLongTime formats a time as "YYYY-MM-DD HH:MM:SS MST".
func fmtLongTime(t time.Time) string {
	return t.Format("2006-01-02 15:04:05 MST")
}

// truncHash shows the first 16 chars of a hash.
func truncHash(h string) string {
	if len(h) > 16 {
		return h[:16]
	}
	return h
}

// buildPageURL constructs a /admin/events URL for pagination with current filters.
func buildPageURL(f FilterState, page int) string {
	v := url.Values{}
	if f.Status != "" {
		v.Set("status", f.Status)
	}
	if f.Source != "" {
		v.Set("source", f.Source)
	}
	if f.Q != "" {
		v.Set("q", f.Q)
	}
	v.Set("page", fmt.Sprintf("%d", page))
	return "/admin/events?" + v.Encode()
}
