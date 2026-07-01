package asciinema

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"syscall"
)

const (
	ptyQueryCPR      = "\x1b[6n"
	ptyQueryOSC11    = "\x1b]11;?\x1b\\"
	ptyOSC11Dummy    = "\x1b]11;rgb:0000/0000/0000\x1b\\"
	ptyQueryCarryMax = 16
)

func ptyCPRDummyResponse() string {
	return fmt.Sprintf("\x1b[%d;%dR", defaultCastRows, defaultCastCols)
}

// copyPTYWithQueryResponses reads PTY output, answers terminal queries on the slave side,
// and writes the captured stream to dest.
func copyPTYWithQueryResponses(ptmx *os.File, dest io.Writer) (int64, error) {
	buf := make([]byte, 32*1024)
	var carry []byte
	var total int64

	for {
		n, err := ptmx.Read(buf)
		if n > 0 {
			carry = append(carry, buf[:n]...)
			carry = respondPTYQueriesInBuffer(ptmx, carry)
			stable, tail := splitPTYQueryCarry(carry)
			carry = tail
			if len(stable) > 0 {
				written, werr := dest.Write(stable)
				total += int64(written)
				if werr != nil {
					return total, werr
				}
			}
		}
		if err != nil {
			if isPTYReadClosed(err) {
				break
			}
			return total, err
		}
	}

	if len(carry) > 0 {
		carry = respondPTYQueriesInBuffer(ptmx, carry)
		if len(carry) > 0 {
			written, werr := dest.Write(carry)
			total += int64(written)
			if werr != nil {
				return total, werr
			}
		}
	}
	return total, nil
}

// isPTYReadClosed reports whether the PTY master read ended because the slave closed (EOF or EIO).
func isPTYReadClosed(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.EOF) || errors.Is(err, syscall.EIO) {
		return true
	}
	return strings.Contains(err.Error(), "input/output error")
}

// respondPTYQueriesInBuffer writes dummy replies for known queries and removes them from the stream.
func respondPTYQueriesInBuffer(ptmx *os.File, data []byte) []byte {
	s := string(data)
	for {
		idxCPR := strings.Index(s, ptyQueryCPR)
		idxOSC := strings.Index(s, ptyQueryOSC11)
		if idxCPR < 0 && idxOSC < 0 {
			return []byte(s)
		}
		var idx int
		var query, response string
		switch {
		case idxCPR >= 0 && (idxOSC < 0 || idxCPR <= idxOSC):
			idx, query, response = idxCPR, ptyQueryCPR, ptyCPRDummyResponse()
		default:
			idx, query, response = idxOSC, ptyQueryOSC11, ptyOSC11Dummy
		}
		_, _ = ptmx.Write([]byte(response))
		s = s[:idx] + s[idx+len(query):]
	}
}

func splitPTYQueryCarry(data []byte) (stable, carry []byte) {
	lastEsc := bytes.LastIndexByte(data, '\x1b')
	if lastEsc < 0 {
		return data, nil
	}
	suffix := data[lastEsc:]
	if isPTYQueryPrefix(suffix) {
		return data[:lastEsc], suffix
	}
	return data, nil
}

func isPTYQueryPrefix(b []byte) bool {
	if len(b) > ptyQueryCarryMax {
		return false
	}
	s := string(b)
	for _, full := range []string{ptyQueryCPR, ptyQueryOSC11} {
		if len(s) <= len(full) && strings.HasPrefix(full, s) {
			return true
		}
	}
	return false
}
