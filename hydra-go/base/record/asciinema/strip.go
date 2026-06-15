package asciinema

import (
	"io"
	"strings"
)

// recordingFilterCarryMax is the suffix kept across Write calls so split escape sequences can be stripped.
const recordingFilterCarryMax = 256

// StripRecordingControlSequences removes terminal query/response sequences that must not
// appear in documentation casts or on stdout while mirroring (OSC 11, DECRQCPR / CPR).
func StripRecordingControlSequences(data string) string {
	data = strings.ReplaceAll(data, "\x1b]11;?\x1b\\", "")
	data = strings.ReplaceAll(data, "\x1b[6n", "")
	data = stripOSC11Queries(data)
	data = stripCPRResponses(data)
	return data
}

// stripCPRResponses removes cursor-position report replies (CSI row;col R) elicited by ESC [ 6 n.
func stripCPRResponses(data string) string {
	var b strings.Builder
	b.Grow(len(data))
	i := 0
	for i < len(data) {
		if i+3 < len(data) && data[i] == '\x1b' && data[i+1] == '[' {
			j := i + 2
			for j < len(data) && data[j] >= '0' && data[j] <= '9' {
				j++
			}
			if j < len(data) && data[j] == ';' {
				j++
				for j < len(data) && data[j] >= '0' && data[j] <= '9' {
					j++
				}
				if j < len(data) && data[j] == 'R' {
					i = j + 1
					continue
				}
			}
		}
		b.WriteByte(data[i])
		i++
	}
	return b.String()
}

// isExitCodeOnlyLine reports lines that only echo a numeric shell exit status (e.g. "0\r\n"
// from PROMPT_COMMAND='echo $?' or terminal multiplexers such as Zellij).
func isExitCodeOnlyLine(line string) bool {
	if !strings.HasSuffix(line, "\n") && !strings.HasSuffix(line, "\r") {
		return false
	}
	content := strings.TrimRight(line, "\r\n")
	if content == "" {
		return false
	}
	for _, r := range content {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// stripOSC11Queries removes any remaining OSC 11 sequences (ESC ] 11 … BEL or ST).
func stripOSC11Queries(data string) string {
	const marker = "\x1b]11"
	for {
		i := strings.Index(data, marker)
		if i < 0 {
			return data
		}
		j := i + len(marker)
		if k := strings.Index(data[j:], "\x1b\\"); k >= 0 {
			data = data[:i] + data[j+k+len("\x1b\\"):]
			continue
		}
		if k := strings.Index(data[j:], "\x07"); k >= 0 {
			data = data[:i] + data[j+k+1:]
			continue
		}
		data = data[:i] + data[i+len(marker):]
	}
}

// RecordingFilterWriter strips terminal query/response escapes before forwarding to the sink.
type RecordingFilterWriter struct {
	w     io.Writer
	carry []byte
}

func NewRecordingFilterWriter(w io.Writer) *RecordingFilterWriter {
	return &RecordingFilterWriter{w: w}
}

func (f *RecordingFilterWriter) Write(p []byte) (int, error) {
	f.carry = append(f.carry, p...)
	for len(f.carry) > recordingFilterCarryMax {
		stable := f.carry[:len(f.carry)-recordingFilterCarryMax]
		f.carry = f.carry[len(f.carry)-recordingFilterCarryMax:]
		if _, err := f.w.Write([]byte(StripRecordingControlSequences(string(stable)))); err != nil {
			return len(p), err
		}
	}
	return len(p), nil
}

func (f *RecordingFilterWriter) Flush() error {
	if len(f.carry) == 0 {
		return nil
	}
	cleaned := StripRecordingControlSequences(string(f.carry))
	f.carry = nil
	if len(cleaned) == 0 {
		return nil
	}
	_, err := f.w.Write([]byte(cleaned))
	return err
}
