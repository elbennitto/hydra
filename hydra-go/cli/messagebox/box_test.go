package messagebox

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRender_UsesAnsiColors(t *testing.T) {
	box, err := Render("Hint", "Example")
	require.NoError(t, err)

	assert.Contains(t, box, "\x1b[")
	assert.Contains(t, box, "Hint")
	assert.Contains(t, box, "Example")
}

func TestRender_DefaultsTitle(t *testing.T) {
	box, err := Render("", "Example")
	require.NoError(t, err)

	assert.Contains(t, box, "Hint")
}

func TestRender_MultilineText(t *testing.T) {
	box, err := Render("Hint", "One\nTwo")
	require.NoError(t, err)

	assert.True(t, strings.Contains(box, "One"))
	assert.True(t, strings.Contains(box, "Two"))
}

func TestRender_StylesFirstContentLineAsTitle(t *testing.T) {
	box, err := Render("HINT:", "Example")
	require.NoError(t, err)

	assert.Regexp(t, regexp.MustCompile(`\x1b\[1;38;5;16;48;5;228m\s+HINT:`), box)
}
