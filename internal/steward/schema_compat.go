package steward

import (
	"encoding/json"
	"errors"
)

func (index SecretIndex) MarshalJSON() ([]byte, error) {
	type disk struct {
		Schema int                  `json:"schema"`
		Refs   map[string]SecretRef `json:"refs"`
	}
	return json.Marshal(disk{Schema: SecretIndexSchema, Refs: index.Refs})
}

func (index *SecretIndex) UnmarshalJSON(data []byte) error {
	type disk struct {
		Schema int                  `json:"schema"`
		Refs   map[string]SecretRef `json:"refs"`
	}
	var decoded disk
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	if decoded.Schema != SecretIndexSchema {
		return errors.New("secret index schema must be 1")
	}
	index.Schema = SecretIndexSchema
	index.Refs = decoded.Refs
	return nil
}

func (observed ObservedState) MarshalJSON() ([]byte, error) {
	type disk ObservedState
	copy := disk(observed)
	copy.Schema = ObservedSchema
	return json.Marshal(copy)
}
