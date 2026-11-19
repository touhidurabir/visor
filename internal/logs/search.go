package logs

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/touhidurabir/visor/internal/supervisor"
)

// ErrNotText means supervisord could not send part of a log, which happens
// when it holds bytes that aren't UTF-8 text.
var ErrNotText = errors.New("supervisord could not send part of this log; it may hold bytes that aren't UTF-8 text")

// SearchClient is the part of *supervisor.Client SearchBack needs: the
// tail RPC (for the log's size) and the read RPC.
type SearchClient interface {
	TailClient
	ReadProcessStdoutLog(ctx context.Context, name string, offset, length int) (string, error)
	ReadProcessStderrLog(ctx context.Context, name string, offset, length int) (string, error)
}

// MaxMatchLength is how much of a matching line SearchBack returns.
const MaxMatchLength = 2000

// SearchPage is one step of a search back through a log.
type SearchPage struct {
	// Lines are the whole lines in this step that match, newest first.
	Lines []string
	// Next is the offset to pass as before for the next step; 0 when the
	// start of the log has been searched.
	Next int
	// Size is the log's size when before was negative (the first step),
	// otherwise 0.
	Size int
	// Capped means Lines reached the limit and the rest of this step was not
	// searched.
	Capped bool
}

// SearchBack searches the step bytes of a log that end at before (negative:
// the end of the log) for query, literally and ignoring case, and returns
// at most limit matching lines, newest first. Steps end on line starts, so a
// line is searched once, whole, unless it is longer than step.
func SearchBack(ctx context.Context, c SearchClient, process string, s Stream, query string, before, step, limit int) (SearchPage, error) {
	var page SearchPage
	if before < 0 {
		r, err := fetch(ctx, c, process, s, 0, 1)
		if err != nil {
			return page, err
		}
		before, page.Size = r.Offset, r.Offset
	}
	if before == 0 || query == "" {
		return page, nil
	}
	start := max(before-step, 0)
	text, start, err := read(ctx, c, process, s, start, before)
	if errors.Is(err, supervisor.ErrInternal) {
		return page, ErrNotText
	}
	if err != nil {
		return page, err
	}
	if start > 0 {
		// The first line may have begun earlier: leave it to the next step.
		// (An XML parser can drop a \r of a \r\n, so offsets taken from text
		// can be a byte short; a step then ends inside a line ending, which
		// only adds or splits one line there.)
		// The newline ending the chunk ends its last line, not the first.
		if i := strings.IndexByte(strings.TrimSuffix(text, "\n"), '\n'); i >= 0 {
			page.Next = start + i + 1
			text = text[i+1:]
		} else {
			page.Next = start // one line longer than step: searched in pieces
		}
	}
	needle := strings.ToLower(query)
	text = strings.TrimSuffix(text, "\n")
	for end := len(text); end >= 0; {
		i := strings.LastIndexByte(text[:end], '\n')
		line := strings.TrimSuffix(text[i+1:end], "\r")
		if strings.Contains(strings.ToLower(line), needle) {
			if len(page.Lines) == limit {
				page.Capped = true
				break
			}
			page.Lines = append(page.Lines, clip(line))
		}
		end = i
	}
	return page, nil
}

// read reads [start, end) of a log. supervisord fails a read that starts
// inside a UTF-8 character, so a failed read moves start back a byte, up
// to three times. It returns the start it read from.
func read(ctx context.Context, c SearchClient, process string, s Stream, start, end int) (string, int, error) {
	for tries := 0; ; tries++ {
		var text string
		var err error
		if s == Stderr {
			text, err = c.ReadProcessStderrLog(ctx, process, start, end-start)
		} else {
			text, err = c.ReadProcessStdoutLog(ctx, process, start, end-start)
		}
		if err == nil || start == 0 || tries == utf8.UTFMax-1 || ctx.Err() != nil {
			return text, start, err
		}
		start--
	}
}

func clip(line string) string {
	if len(line) <= MaxMatchLength {
		return line
	}
	return strings.ToValidUTF8(line[:MaxMatchLength], "") + "…"
}
