package commands

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"hydra-gitops.org/hydra/hydra-go/core/types"
)

func TestPickerRowStatusLocalTemplateOnly(t *testing.T) {
	id := types.Id("v1/ConfigMap/ns/cm")
	m := PickerRowStatusLocalTemplateOnly([]types.Id{id})
	assert.Equal(t, "missing", m[id])
}
