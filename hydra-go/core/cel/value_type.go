package cel

import (
	ctypes "github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"hydra-gitops.org/hydra/hydra-go/base/errors"
	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/types"
)

type reader interface {
	Read(entity.Entity) ref.Val
	Type() *ctypes.Type
}

type readerString[T ~string] struct {
	key         types.EntityKeyString
	keyNotFound bool
	getter      func(entity.Entity) (T, error)
}

func newReaderString[T ~string](key types.EntityKeyString, keyNotFound bool, getter func(entity.Entity) (T, error)) reader {
	return readerString[T]{key: key, keyNotFound: keyNotFound, getter: getter}
}

var _ reader = readerString[string]{}

func (v readerString[T]) Read(e entity.Entity) ref.Val {
	result, err := v.getter(e)
	if errors.ErrKeyNotFound.MatchesError(err) {
		if v.keyNotFound {
			return ctypes.WrapErr(err)
		}
		return ctypes.String("")
	}
	return ctypes.String(string(result))
}

func (readerString[T]) Type() *ctypes.Type {
	return ctypes.StringType
}

type readerBool[T ~bool] struct {
	key         types.EntityKeyBool
	keyNotFound bool
	getter      func(entity.Entity) (T, error)
}

func newReaderBool[T ~bool](key types.EntityKeyBool, keyNotFound bool, getter func(entity.Entity) (T, error)) reader {
	return readerBool[T]{key: key, keyNotFound: keyNotFound, getter: getter}
}

var _ reader = readerBool[bool]{}

func (v readerBool[T]) Read(e entity.Entity) ref.Val {
	result, err := v.getter(e)
	if errors.ErrKeyNotFound.MatchesError(err) {
		if v.keyNotFound {
			return ctypes.WrapErr(err)
		}
		return ctypes.Bool(false)
	}
	return ctypes.Bool(bool(result))
}

func (readerBool[T]) Type() *ctypes.Type {
	return ctypes.BoolType
}

type readerBoolNoErr[T ~bool] struct {
	key         types.EntityKeyBool
	keyNotFound bool
	getter      func(entity.Entity) T
}

func newReaderBoolNoErr[T ~bool](key types.EntityKeyBool, keyNotFound bool, getter func(entity.Entity) T) reader {
	return readerBoolNoErr[T]{key: key, keyNotFound: keyNotFound, getter: getter}
}

var _ reader = readerBoolNoErr[bool]{}

func (v readerBoolNoErr[T]) Read(e entity.Entity) ref.Val {
	return ctypes.Bool(bool(v.getter(e)))
}

func (readerBoolNoErr[T]) Type() *ctypes.Type {
	return ctypes.BoolType
}

type readerSlice[T ~string] struct {
	key         types.EntityKeySlice[T]
	keyNotFound bool
	getter      func(entity.Entity) ([]T, error)
}

func newReaderSlice[T ~string](key types.EntityKeySlice[T], keyNotFound bool, getter func(entity.Entity) ([]T, error)) reader {
	return readerSlice[T]{key: key, keyNotFound: keyNotFound, getter: getter}
}

var _ reader = readerSlice[string]{}

func (v readerSlice[T]) Read(e entity.Entity) ref.Val {
	result, err := v.getter(e)
	if errors.ErrKeyNotFound.MatchesError(err) {
		if v.keyNotFound {
			return ctypes.WrapErr(err)
		}
		return ctypes.NewStringList(ctypes.DefaultTypeAdapter, []string{})
	}
	strings := []string{}
	for _, r := range result {
		strings = append(strings, string(r))
	}
	return ctypes.NewStringList(ctypes.DefaultTypeAdapter, strings)
}

func (readerSlice[T]) Type() *ctypes.Type {
	return ctypes.NewListType(ctypes.StringType)
}

type readerUnstructured struct {
	key types.EntityKeyUnstructured
}

func newReaderUnstructured(key types.EntityKeyUnstructured) reader {
	return readerUnstructured{key: key}
}

var _ reader = readerUnstructured{}

func (v readerUnstructured) Read(e entity.Entity) ref.Val {
	u, found := e.ReadOnlyUnstructured(v.key)
	if !found {
		return ctypes.NullValue
	}
	if len(u.Object) == 0 {
		return ctypes.NewDynamicMap(
			ctypes.DefaultTypeAdapter,
			map[string]any{},
		)
	}
	return ctypes.NewDynamicMap(
		ctypes.DefaultTypeAdapter,
		u.Object,
	)
}

func (readerUnstructured) Type() *ctypes.Type {
	return ctypes.NewNullableType(ctypes.NewMapType(ctypes.StringType, ctypes.DynType))
}

func readers() map[types.EntityKey]reader {
	result := map[types.EntityKey]reader{
		types.KeyApiVersion:   newReaderString(types.KeyApiVersion, false, entity.Entity.ApiVersionString),
		types.KeyAppIds:       newReaderSlice(types.KeyAppIds, false, entity.Entity.AppIds),
		types.KeyAppNamespace: newReaderString(types.KeyAppNamespace, false, entity.Entity.AppNamespace),
		types.KeyGroup:        newReaderString(types.KeyGroup, false, entity.Entity.Group),
		types.KeyGVK:          newReaderString(types.KeyGVK, false, entity.Entity.GVKString),
		types.KeyGVKN:         newReaderString(types.KeyGVKN, false, entity.Entity.GVKNString),
		types.KeyGVR:          newReaderString(types.KeyGVR, false, entity.Entity.GVRString),
		types.KeyId:           newReaderString(types.KeyId, false, entity.Entity.Id),
		types.KeyKind:         newReaderString(types.KeyKind, false, entity.Entity.Kind),
		types.KeyName:         newReaderString(types.KeyName, true, entity.Entity.Name),
		types.KeyNamespace:    newReaderString(types.KeyNamespace, false, entity.Entity.Namespace),
		types.KeyNamespaced:   newReaderBool(types.KeyNamespaced, false, entity.Entity.Namespaced),
		types.KeyResource:     newReaderString(types.KeyResource, false, entity.Entity.Resource),
		types.KeySelected:     newReaderBoolNoErr(types.KeySelected, false, entity.Entity.Selected),
		types.KeyVerbs:        newReaderSlice(types.KeyVerbs, false, entity.Entity.Verbs),
		types.KeyVersion:      newReaderString(types.KeyVersion, false, entity.Entity.Version),
	}
	for key := range types.EntityKeys() {
		if uk, ok := key.(types.EntityKeyUnstructured); ok {
			result[uk] = newReaderUnstructured(uk)
		}
	}
	return result
}
