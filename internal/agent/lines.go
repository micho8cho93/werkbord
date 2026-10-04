package agent

import (
	"bufio"
	"errors"
	"io"
)

// maxLineBytes bounds one line of agent output. Agents print whole tool
// results on one line of JSON, which can be large; beyond this the line is
// cut rather than held in memory.
const maxLineBytes = 8 << 20

// readLines calls fn for each line of r, without the newline, until r ends or
// fails. A line longer than max is passed with truncated set, holding its first
// max bytes; the rest of it is discarded. fn must not keep the slice.
func readLines(r io.Reader, max int, fn func(line []byte, truncated bool)) error {
	br := bufio.NewReaderSize(r, 64<<10)
	var buf []byte
	truncated := false
	for {
		chunk, err := br.ReadSlice('\n')
		if room := max - len(buf); room > 0 {
			if len(chunk) > room {
				chunk, truncated = chunk[:room], true
			}
			buf = append(buf, chunk...)
		} else if len(chunk) > 0 {
			truncated = true
		}
		switch {
		case err == nil:
			fn(trimEOL(buf), truncated)
			buf, truncated = buf[:0], false
		case errors.Is(err, bufio.ErrBufferFull):
			// The line continues in the next chunk.
		default:
			if len(buf) > 0 {
				fn(trimEOL(buf), truncated)
			}
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
	}
}

func trimEOL(b []byte) []byte {
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r') {
		b = b[:len(b)-1]
	}
	return b
}
