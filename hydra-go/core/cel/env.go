package cel

import (
	"fmt"
	"strings"

	"hydra-gitops.org/hydra/hydra-go/base/log"

	"github.com/google/cel-go/cel"
	ctypes "github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/ext"
	"hydra-gitops.org/hydra/hydra-go/base/errors"
	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/types"
)

const maxCelLogSnippet = 4000

// Shown when compile() was called without a logical source (prefer CompilePredicateAt / CompileSelectedPredicateAt).
const celEmptyPredicateNoOriginHint = " (no source location passed to compiler — use Hydra APIs that take an explicit origin string, or report this as a bug)"

func truncateCelSnippet(s string) string {
	if len(s) <= maxCelLogSnippet {
		return s
	}
	return s[:maxCelLogSnippet] + "…"
}

// celExprLabel formats a CEL source string for human-readable errors (never truly empty).
func celExprLabel(code string) string {
	if strings.TrimSpace(code) == "" {
		return "<empty>"
	}
	return truncateCelSnippet(code)
}

// refSelectorOrigin is a short hint for CEL errors (API version + resource name), or "" if unconstrained.
func refSelectorOrigin(s types.RefSelector) string {
	if s.IsZero() {
		return ""
	}
	var segs []string
	g := strings.Trim(strings.Join([]string{string(s.Group), string(s.Version), string(s.Kind)}, "/"), "/")
	if g != "" {
		segs = append(segs, g)
	}
	switch {
	case s.Namespace != "" && s.Name != "":
		segs = append(segs, string(s.Namespace)+"/"+string(s.Name))
	case s.Namespace != "":
		segs = append(segs, "ns="+string(s.Namespace))
	case s.Name != "":
		segs = append(segs, "name="+string(s.Name))
	}
	return strings.Join(segs, " ")
}

func mergeCelLogicalOrigin(logicalSource, selectorHint string) string {
	logicalSource = strings.TrimSpace(logicalSource)
	selectorHint = strings.TrimSpace(selectorHint)
	switch {
	case logicalSource != "" && selectorHint != "":
		return logicalSource + "; resource filter " + selectorHint
	case logicalSource != "":
		return logicalSource
	default:
		return selectorHint
	}
}

type Env struct {
	env            *cel.Env
	keyTypes       map[types.EntityKey]reader
	keyByNameCache map[string]types.EntityKey
}

func NewEnv(extraOptions ...cel.EnvOption) (Env, error) {
	options := []cel.EnvOption{
		cel.OptionalTypes(),
		cel.EnableIdentifierEscapeSyntax(),
		cel.ASTValidators(
			cel.ValidateDurationLiterals(),
			cel.ValidateTimestampLiterals(),
			cel.ValidateRegexLiterals(),
		),
		ext.Encoders(),
		ext.Strings(),
		ext.Lists(),
		ext.Regex(),
		ext.Math(),
		ext.Sets(),
		EntitySupport(),
		UtilSupport(),
	}

	options = append(options, extraOptions...)

	env, err := cel.NewEnv(options...)
	if err != nil {
		l := log.Default()
		l.Error(logIdCel, "CEL new environment failed: {err}", log.Err(err))
		return Env{}, log.CreateError(errors.ErrCelNewEnvFailed, "failed to create cel env", log.Err(err))
	}

	keyTypes := readers()
	keyByNameCache := make(map[string]types.EntityKey, len(keyTypes))
	for key := range keyTypes {
		keyByNameCache[key.String()] = key
	}

	return Env{env: env, keyTypes: keyTypes, keyByNameCache: keyByNameCache}, nil
}

func (e *Env) compile(code string, origin string) (program, error) {
	if strings.TrimSpace(code) == "" {
		msg := "CEL predicate must not be empty or whitespace-only (got {expression})"
		args := []any{log.String("expression", celExprLabel(code))}
		if origin != "" {
			msg = "{origin}: " + msg
			args = append([]any{log.String("origin", origin)}, args...)
		} else {
			msg += celEmptyPredicateNoOriginHint
		}
		return program{}, log.CreateError(errors.ErrCelCompileFailed, msg, args...)
	}

	ast, issues := e.env.Compile(code)
	if issues != nil && issues.Err() != nil {
		compileErr := issues.Err()
		l := log.Default()
		logArgs := []any{
			log.Err(compileErr),
			log.String("expression", celExprLabel(code)),
		}
		if origin != "" {
			logArgs = append(logArgs, log.String("origin", origin))
		}
		l.Error(logIdCel, "CEL compile failed (type-check): {err}", logArgs...)
		msg := "CEL compile failed for expression {expression}: {detail}"
		args := []any{
			log.String("expression", celExprLabel(code)),
			log.String("detail", compileErr.Error()),
		}
		if origin != "" {
			msg = "{origin}: " + msg
			args = append([]any{log.String("origin", origin)}, args...)
		}
		return program{}, log.CreateError(errors.ErrCelCompileFailed, msg, args...)
	}

	p, err := e.env.Program(ast)
	if err != nil {
		l := log.Default()
		logArgs := []any{
			log.Err(err),
			log.String("expression", celExprLabel(code)),
		}
		if origin != "" {
			logArgs = append(logArgs, log.String("origin", origin))
		}
		l.Error(logIdCel, "CEL program construction failed: {err}", logArgs...)
		msg := "CEL program construction failed for expression {expression}: {detail}"
		args := []any{
			log.String("expression", celExprLabel(code)),
			log.String("detail", err.Error()),
		}
		if origin != "" {
			msg = "{origin}: " + msg
			args = append([]any{log.String("origin", origin)}, args...)
		}
		return program{}, log.CreateError(errors.ErrCelProgramFailed, msg, args...)
	}

	return program{
		env:     e,
		code:    code,
		program: p,
	}, nil
}

func (e *Env) CompileExpression(expression types.CelExpression) (Expression, error) {
	return e.CompileExpressionAt("", expression)
}

// CompileExpressionAt compiles a pick/macro expression; logicalSource should name the command or config entry (e.g. hydra find --pick).
func (e *Env) CompileExpressionAt(logicalSource string, expression types.CelExpression) (Expression, error) {
	return e.compile(string(expression), strings.TrimSpace(logicalSource))
}

func (e *Env) CompileSelectedPredicate(selector types.RefSelector, predicates ...types.CelPredicate) (Predicate, error) {
	return e.CompileSelectedPredicateAt("", selector, predicates...)
}

// CompileSelectedPredicateAt is like [CompileSelectedPredicate] but prefixes errors with logicalSource (preset location, ref-parser desc, CLI flag, …).
func (e *Env) CompileSelectedPredicateAt(logicalSource string, selector types.RefSelector, predicates ...types.CelPredicate) (Predicate, error) {
	if len(predicates) == 0 {
		return selectedPredicate{selector: selector, delegate: nil}, nil
	}
	result := []program{}
	selOrigin := refSelectorOrigin(selector)
	for i, predicate := range predicates {
		origin := mergeCelLogicalOrigin(logicalSource, selOrigin)
		if len(predicates) > 1 {
			clause := fmt.Sprintf("clause[%d]", i)
			if origin != "" {
				origin = origin + ", " + clause
			} else {
				origin = clause
			}
		}
		program, err := e.compile(string(predicate), origin)
		if err != nil {
			return nil, err
		}
		result = append(result, program)
	}
	if len(result) == 1 {
		return selectedPredicate{selector: selector, delegate: result[0]}, nil
	}
	return selectedPredicate{
		selector: selector,
		delegate: programs{
			env:      e,
			programs: result,
		},
	}, nil
}

func (e *Env) CompilePredicate(predicates ...types.CelPredicate) (Predicate, error) {
	return e.CompilePredicateAt("", predicates...)
}

// CompilePredicateAt compiles one or more predicate clauses; logicalSource should name the command or config entry.
func (e *Env) CompilePredicateAt(logicalSource string, predicates ...types.CelPredicate) (Predicate, error) {
	return e.CompileSelectedPredicateAt(logicalSource, types.RefSelector{}, predicates...)
}

func (e Env) keyGetter(key types.EntityKey) func(entity.Entity) ref.Val {
	reader, celValueTypeFound := e.keyTypes[key]

	return func(e entity.Entity) ref.Val {
		if !celValueTypeFound {
			return ctypes.WrapErr(
				log.CreateError(errors.ErrEvaluationFailed, "no CEL reader found for key '{key}'",
					log.String("key", key.String())))
		}
		return reader.Read(e)
	}
}

func (env Env) EntityToMap(e entity.Entity) map[string]any {
	m := map[string]any{}

	for key := range types.EntityKeys() {
		getter := env.keyGetter(key)
		k := key.String()
		v := getter(e)
		m[k] = v
	}

	return m
}

func (p program) eval(input any) (ref.Val, error) {
	refVal, details, err := p.program.Eval(input)
	if err != nil {
		unwrapped := err.(*ctypes.Err).Unwrap()

		if errors.IsKnownError(unwrapped) {
			return nil, unwrapped
		}

		if strings.HasPrefix(err.Error(), "no such key:") {
			return nil, log.CreateError(errors.ErrKeyNotFound, "key not found while evaluating '{code}': {err}",
				log.Err(err), log.Any("details", details),
				log.String("code", p.code))
		}

		// Treat null-related iteration errors as "key not found" — happens when
		// a field like containers is null and .map()/.flatten() is called on it.
		if strings.Contains(err.Error(), "got 'types.Null'") {
			return nil, log.CreateError(errors.ErrKeyNotFound, "null value while evaluating '{code}': {err}",
				log.Err(err), log.Any("details", details),
				log.String("code", p.code))
		}

		l := log.Default()
		l.Error(logIdCel, "CEL evaluation failed: {err}",
			log.Err(err),
			log.String("expression", truncateCelSnippet(p.code)),
			log.Any("details", details),
		)
		return nil, log.CreateError(errors.ErrEvaluationFailed, "error evaluating '{code}': {err}",
			log.Err(err), log.Any("details", details),
			log.String("code", p.code))
	}

	return refVal, nil
}

func (p program) evalBool(input any, missingKeys types.MissingKeys) (bool, error) {
	refVal, err := p.eval(input)

	if errors.ErrKeyNotFound.MatchesError(err) {
		switch missingKeys {
		case types.MissingKeysReject:
			return false, nil
		case types.MissingKeysAccept:
			return true, nil
		default:
			return false, err
		}
	}

	if err != nil {
		return false, err
	}

	b, ok := refVal.Value().(bool)
	if !ok {
		l := log.Default()
		l.Error(logIdCel, "CEL predicate did not return boolean: expression={expression} result={result}",
			log.String("expression", truncateCelSnippet(p.code)),
			log.Any("result", refVal),
			log.Any("entity", input),
		)
		return false, log.CreateError(errors.ErrDidNotEvalToBool,
			"code '{code}' did not return a boolean result for entity {entity}: got '{result}'",
			log.String("code", p.code),
			log.Any("result", refVal),
			log.Any("entity", input),
		)
	}

	return b, nil
}
