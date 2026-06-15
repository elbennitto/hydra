package record

import (
	"fmt"
	"regexp"
	"strings"

	goocel "github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/ext"
	"hydra-gitops.org/hydra/hydra-go/base/record/directive"
)

var c1ColorEscapePattern = regexp.MustCompile(`\x9b[0-9;:]*m`)

func evalAssertionExpr(expr string, history string, stdout string, stderr string, envVars map[string]string) error {
	result, err := evalCEL(expr, history, stdout, stderr, envVars)
	if err != nil {
		return err
	}
	if !result {
		trimmed := strings.TrimSpace(history)
		if len(trimmed) > 500 {
			trimmed = trimmed[:500] + "..."
		}
		return fmt.Errorf("CEL assertion failed: %s\nhistory:\n%s", strings.TrimSpace(expr), trimmed)
	}
	return nil
}

func plain(text string) string {
	if text == "" {
		return ""
	}
	text = stripANSICodes(text)
	text = c1ColorEscapePattern.ReplaceAllString(text, "")
	text, _ = directive.StripSleepDirectives(text)
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	return text
}

func evalEnvCEL(expr string, envVars map[string]string) (string, error) {
	env, err := goocel.NewEnv(
		goocel.Variable("env", goocel.MapType(goocel.StringType, goocel.StringType)),
		goocel.Variable("pwd", goocel.StringType),
		ext.Strings(),
		ext.Lists(),
	)
	if err != nil {
		return "", fmt.Errorf("create CEL env: %w", err)
	}

	ast, issues := env.Compile(expr)
	if issues != nil && issues.Err() != nil {
		return "", fmt.Errorf("compile CEL expression %q: %w", expr, issues.Err())
	}

	prg, err := env.Program(ast)
	if err != nil {
		return "", fmt.Errorf("build CEL expression %q: %w", expr, err)
	}

	out, _, err := prg.Eval(map[string]any{
		"env": envVars,
		"pwd": envVars["PWD"],
	})
	if err != nil {
		return "", fmt.Errorf("evaluate CEL expression %q: %w", expr, err)
	}

	return fmt.Sprint(out.Value()), nil
}

func evalCEL(expr string, history string, stdout string, stderr string, envVars map[string]string) (bool, error) {
	lines := splitVisibleLines(history)
	cast := stdout + stderr
	env, err := goocel.NewEnv(
		goocel.Function("plain",
			goocel.Overload(
				"record_plain_string",
				[]*goocel.Type{goocel.StringType},
				goocel.StringType,
				goocel.UnaryBinding(func(value ref.Val) ref.Val {
					input, ok := value.(types.String)
					if !ok {
						return types.NewErr("plain expects string input")
					}
					return types.String(plain(string(input)))
				}),
			),
		),
		goocel.Variable("history", goocel.StringType),
		goocel.Variable("stdout", goocel.StringType),
		goocel.Variable("stderr", goocel.StringType),
		goocel.Variable("cast", goocel.StringType),
		goocel.Variable("lines", goocel.ListType(goocel.StringType)),
		goocel.Variable("env", goocel.MapType(goocel.StringType, goocel.StringType)),
		goocel.Variable("pwd", goocel.StringType),
		ext.Strings(),
		ext.Lists(),
	)
	if err != nil {
		return false, fmt.Errorf("create CEL env: %w", err)
	}

	ast, issues := env.Compile(expr)
	if issues != nil && issues.Err() != nil {
		return false, fmt.Errorf("compile CEL assertion %q: %w", expr, issues.Err())
	}

	prg, err := env.Program(ast)
	if err != nil {
		return false, fmt.Errorf("build CEL assertion %q: %w", expr, err)
	}

	out, _, err := prg.Eval(map[string]any{
		"history": history,
		"stdout":  stdout,
		"stderr":  stderr,
		"cast":    cast,
		"lines":   lines,
		"env":     envVars,
		"pwd":     envVars["PWD"],
	})
	if err != nil {
		return false, fmt.Errorf("evaluate CEL assertion %q: %w", expr, err)
	}

	value := out.Value()
	ok, castOK := value.(bool)
	if !castOK {
		return false, fmt.Errorf("CEL assertion %q returned %T, want bool", expr, value)
	}
	return ok, nil
}
