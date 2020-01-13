package merge

import (
	"encoding/json"
	"reflect"
)

type BoolInSetting struct {
	Set   bool
	Value bool
}

func (b *BoolInSetting) UnmarshalJSON(data []byte) error {
	b.Set = true

	err := json.Unmarshal(data, &b.Value)
	return err
}

type BoolInSettingTransformer struct {
}

func (t BoolInSettingTransformer) Transformer(typ reflect.Type) func(dst, src reflect.Value) error {
	if (typ == reflect.TypeOf(BoolInSetting{})) {
		return func(dst, src reflect.Value) error {
			if dst.CanSet() && src.FieldByName("Set").Bool() {
				dst.Set(src)
			}
			return nil
		}
	}
	return nil
}
