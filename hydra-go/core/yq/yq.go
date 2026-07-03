package yq

import (
	"hydra-gitops.org/hydra/hydra-go/base/log"

	"github.com/mikefarah/yq/v4/pkg/yqlib"
	"gopkg.in/op/go-logging.v1"
	"hydra-gitops.org/hydra/hydra-go/base/errors"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"hydra-gitops.org/hydra/hydra-go/core/yaml"
	"k8s.io/apimachinery/pkg/runtime"
)

func Yq(yamlString types.YamlString, expr string) (types.YamlString, error) {
	logging.SetLevel(logging.INFO, "yq-lib")
	decoder := yqlib.NewYamlDecoder(yqlib.NewDefaultYamlPreferences())
	encoder := yqlib.NewYamlEncoder(yqlib.NewDefaultYamlPreferences())
	patched, err := yqlib.NewStringEvaluator().Evaluate(expr, string(yamlString), encoder, decoder)
	if err != nil {
		return "", log.CreateError(errors.ErrYqFailed,
			"evaluating yq expression failed: {err}:\n{expr}, expression:",
			log.String("expr", expr), log.Err(err))
	}
	return types.YamlString(patched), nil
}

func YqPatchArgo(
	l log.Logger,
	yamlString types.YamlString,
	appId types.AppId,
	namespace types.Namespace,
) (types.YamlString, error) {
	l.DebugLog(logIdYq, "patching templates with yq")

	return Yq(yamlString, yqPatchExpr(appId, namespace))
}

func ToYaml(color types.Color, data any) (string, error) {
	yamlString, err := yaml.ToYaml(data)
	if err != nil {
		return "", err
	}

	return YamlStringColored(color, yamlString)
}

func PrintObject(
	color types.Color,
	keepServerFields types.KeepServerFields,
	comment *string,
	object runtime.Object,
) (string, error) {
	yamlString, err := yaml.PrintObject(keepServerFields, comment, object)
	if err != nil {
		return "", err
	}

	return YamlStringColored(color, yamlString)
}

// YamlStringColored applies color to YAML output using yq's color encoder
func YamlStringColored(color types.Color, yamlString types.YamlString) (string, error) {
	if !color {
		return string(yamlString), nil
	}

	logging.SetLevel(logging.INFO, "yq-lib")
	decoder := yqlib.NewYamlDecoder(yqlib.NewDefaultYamlPreferences())

	// Create encoder with color enabled
	colorPrefs := yqlib.NewDefaultYamlPreferences()
	colorPrefs.ColorsEnabled = true
	encoder := yqlib.NewYamlEncoder(colorPrefs)

	result, err := yqlib.NewStringEvaluator().Evaluate(".", string(yamlString), encoder, decoder)
	if err != nil {
		return string(yamlString), log.CreateError(errors.ErrYqFailed, "colorizing YAML failed", log.Err(err))
	}
	return string(result), nil
}

func yqPatchExpr(appId types.AppId, namespace types.Namespace) string {
	return `
    .
    |
    (
      select
        (
          (
            . | has("metadata")
          ) and (
            (
              (
                .metadata | has("annotations")
              ) and (
                .metadata.annotations | has("argocd.argoproj.io/tracking-id")
              )
            ) | not
          ) and (
            (
              (
                .apiVersion == "apiextensions.k8s.io/v1"
              ) and (
                .kind == "CustomResourceDefinition"
              )
            ) | not
          )
        )
      |
        .metadata.annotations["argocd.argoproj.io/tracking-id"] =
          "` + string(appId) + `:" + ((.apiVersion | split("/") | select(length > 1) | .[0]) // "") + "/" + .kind + ":" + (.metadata.namespace // "` + string(namespace) + `") + "/" + .metadata.name
    ) // .
    |
    (
      select
        (
          (
            .metadata.annotations["argocd.argoproj.io/tracking-id"] // ""
          ) == "none"
        )
      |
      del
        (
          .metadata.annotations["argocd.argoproj.io/tracking-id"]
        )
    ) // .
	`
}
