package directive

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSleepLineAndParse(t *testing.T) {
	line := SleepLine(1.2)
	assert.Equal(t, "<<hydra sleep 1.2>>", line)

	secs, ok := ParseSleepLine(line)
	require.True(t, ok)
	assert.InDelta(t, 1.2, secs, 1e-9)
}

func TestSleepInlineAndParse(t *testing.T) {
	line := SleepInline(0.25)
	assert.Equal(t, "<<hydra sleep 0.25>>", line)
}

func TestParseSleepLine_RejectsUnknownDirective(t *testing.T) {
	_, ok := ParseSleepLine("<<hydra wait 1>>")
	assert.False(t, ok)
}

func TestParseSleepLine_FindsEmbeddedDirective(t *testing.T) {
	secs, ok := ParseSleepLine("prefix <<hydra sleep 1.5>> suffix")
	require.True(t, ok)
	assert.InDelta(t, 1.5, secs, 1e-9)
}

func TestWriteSleep(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, WriteSleep(&buf, 2))
	assert.Equal(t, "<<hydra sleep 2>>", buf.String())
}

func TestColorResetLine(t *testing.T) {
	assert.Equal(t, "<<hydra color reset>>", ColorResetLine())
}

func TestMarkerLineAndParse(t *testing.T) {
	line := MarkerLine("Deploy app")
	assert.Equal(t, "<<hydra marker Deploy app>>", line)

	label, ok := ParseMarkerLine(line)
	require.True(t, ok)
	assert.Equal(t, "Deploy app", label)
}

func TestParseMarkerLine_FindsEmbeddedDirective(t *testing.T) {
	label, ok := ParseMarkerLine("prefix <<hydra marker Hello marker>> suffix")
	require.True(t, ok)
	assert.Equal(t, "Hello marker", label)
}

func TestIsMarkerDirectiveOnlyLine(t *testing.T) {
	assert.True(t, IsMarkerDirectiveOnlyLine("<<hydra marker Step one>>\r\n"))
	assert.False(t, IsMarkerDirectiveOnlyLine("x<<hydra marker Step one>>\r\n"))
}

func TestIsSleepDirectiveOnlyLine(t *testing.T) {
	assert.True(t, IsSleepDirectiveOnlyLine("<<hydra sleep 1>>\r\n"))
	assert.False(t, IsSleepDirectiveOnlyLine("x<<hydra sleep 1>>\r\n"))
}
