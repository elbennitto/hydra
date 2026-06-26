package errormarkdown

import (
	"bytes"
	"embed"
	"fmt"
	"text/template"

	"hydra-gitops.org/hydra/hydra-go/base/errors"
)

//go:embed templates/*.md.gotpl
var templateFS embed.FS

// HasTemplate reports whether a markdown help template exists for code.
func HasTemplate(code errors.ErrorId) bool {
	if code == errors.ErrUnknown || code == "" {
		return false
	}

	tplPath := fmt.Sprintf("templates/%s.md.gotpl", code)
	_, err := templateFS.ReadFile(tplPath)
	return err == nil
}

// Render renders a markdown explanation from a gotpl template selected by
// error code. Logger parameters are available both at top level and under
// the "params" key.
func Render(code errors.ErrorId, params map[string]any) (string, error) {
	if code == errors.ErrUnknown || code == "" {
		return "", fmt.Errorf("unknown error code")
	}

	tplPath := fmt.Sprintf("templates/%s.md.gotpl", code)
	tplRaw, err := templateFS.ReadFile(tplPath)
	if err != nil {
		return "", fmt.Errorf("error template not found for %s: %w", code, err)
	}

	data := map[string]any{
		"errorCode": string(code),
		"params":    params,
	}
	for k, v := range params {
		data[k] = v
	}

	tpl, err := template.New(string(code)).Option("missingkey=zero").Parse(string(tplRaw))
	if err != nil {
		return "", fmt.Errorf("parse template %s: %w", tplPath, err)
	}

	var out bytes.Buffer
	if err := tpl.Execute(&out, data); err != nil {
		return "", fmt.Errorf("execute template %s: %w", tplPath, err)
	}

	return out.String(), nil
}
