package storage

import "encoding/json"

// State commits related changes together. Managers keep their existing locks
// while validating state and committing, and never hold a transaction over a
// broker request. A nil State selects the existing process-local behaviour.
type State interface {
	Load(bucket string) (map[string]json.RawMessage, error)
	Apply(...Change) error
	Check() error
	Close() error
}

type Change struct {
	Bucket, Key string
	Value       json.RawMessage // nil deletes the record
}

func Put[T any](state State, bucket, key string, value T) error {
	if state == nil {
		return nil
	}
	change, err := Encode(bucket, key, value)
	if err != nil {
		return err
	}
	return state.Apply(change)
}

func Encode[T any](bucket, key string, value T) (Change, error) {
	data, err := json.Marshal(value)
	return Change{Bucket: bucket, Key: key, Value: data}, err
}

func Delete(state State, bucket, key string) error {
	if state == nil {
		return nil
	}
	return state.Apply(Change{Bucket: bucket, Key: key})
}

func Check(state State) error {
	if state == nil {
		return nil
	}
	return state.Check()
}
