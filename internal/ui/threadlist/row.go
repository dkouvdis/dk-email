package threadlist

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/dkouvdis/dk-email/internal/mail"
)

const (
	// "● ★  ": unread dot, star, then two spaces before the sender.
	markersW   = 5
	msgIndent  = "  "
	minSender  = 8
	maxSender  = 24
	senderPct  = 28
	minSubject = 4
)

type rowStyles struct {
	unreadDot, star, bold, dim, cursor lipgloss.Style
}

func defaultRowStyles() rowStyles {
	return rowStyles{
		unreadDot: lipgloss.NewStyle().Foreground(lipgloss.Color("111")),
		star:      lipgloss.NewStyle().Foreground(lipgloss.Color("220")),
		bold:      lipgloss.NewStyle().Bold(true),
		dim:       lipgloss.NewStyle().Foreground(lipgloss.Color("244")),
		cursor:    lipgloss.NewStyle().Reverse(true),
	}
}

// rowData is what one line shows, independent of styling.
type rowData struct {
	unread, starred bool
	message         bool // a message inside an expanded thread
	sender, subject string
	count           int // messages in the thread; shown when > 1
	date            string
}

// columns splits width into sender and subject widths around the fixed parts.
func columns(width, dateW int) (senderW, subjectW int) {
	senderW = min(max(width*senderPct/100, minSender), maxSender)
	subjectW = width - markersW - senderW - 1 - 1 - dateW
	if subjectW < minSubject {
		senderW = max(senderW-(minSubject-subjectW), 1)
		subjectW = max(width-markersW-senderW-2-dateW, 0)
	}
	return senderW, subjectW
}

func (st rowStyles) render(r rowData, width, dateW int, cursor bool) string {
	senderW, subjectW := columns(width, dateW)

	dot, star := " ", " "
	if r.unread {
		dot = "●"
	}
	if r.starred {
		star = "★"
	}

	sender := r.sender
	if r.message {
		sender = msgIndent + sender
	}
	sender = pad(ansi.Truncate(sender, senderW, "…"), senderW)

	count := ""
	if r.count > 1 {
		count = fmt.Sprintf(" (%d)", r.count)
	}
	subjW := max(subjectW-ansi.StringWidth(count), 0)
	subject := ansi.Truncate(r.subject, subjW, "…")
	if subjW == 0 {
		count = ""
	}
	subject = pad(subject+count, subjectW)
	date := padLeft(r.date, dateW)

	if cursor {
		return st.cursor.Render(dot + " " + star + "  " + sender + " " + subject + " " + date)
	}
	if r.unread {
		dot = st.unreadDot.Render(dot)
		sender, subject = st.bold.Render(sender), st.bold.Render(subject)
	}
	if r.starred {
		star = st.star.Render(star)
	}
	if r.message {
		subject = st.dim.Render(subject)
	}
	return dot + " " + star + "  " + sender + " " + subject + " " + st.dim.Render(date)
}

// formatDate shows the time for today, "Jan 2" earlier this year, and the
// full date otherwise, all in now's location.
func formatDate(t, now time.Time) string {
	if t.IsZero() {
		return ""
	}
	t = t.In(now.Location())
	switch {
	case t.Year() == now.Year() && t.YearDay() == now.YearDay():
		return t.Format("15:04")
	case t.Year() == now.Year():
		return t.Format("Jan 2")
	}
	return t.Format("2006-01-02")
}

func senderName(s mail.Summary) string { return s.From.DisplayName() }

func subjectText(s mail.Summary) string {
	if strings.TrimSpace(s.Subject) == "" {
		return "(no subject)"
	}
	return s.Subject
}

func pad(s string, w int) string {
	return s + strings.Repeat(" ", max(w-ansi.StringWidth(s), 0))
}

func padLeft(s string, w int) string {
	return strings.Repeat(" ", max(w-ansi.StringWidth(s), 0)) + s
}
