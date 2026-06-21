package proto

import (
	"encoding/json"

	"google.golang.org/grpc/encoding"
)

func init() {
	encoding.RegisterCodec(JSONCodec{})
}

// JSONCodec implements encoding.Codec using JSON serialization.
// This is required because the Tergum proto types are plain Go structs
// (not generated protobuf messages) and need JSON marshaling over gRPC.
type JSONCodec struct{}

// Marshal serializes a message to JSON bytes.
func (JSONCodec) Marshal(v interface{}) ([]byte, error) {
	if v == nil {
		return nil, nil
	}
	return json.Marshal(v)
}

// Unmarshal deserializes JSON bytes into a message.
func (JSONCodec) Unmarshal(data []byte, v interface{}) error {
	if len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, v)
}

// Name returns the codec name registered with gRPC.
// "proto" replaces the default protobuf codec since our types are not protobuf messages.
func (JSONCodec) Name() string {
	return "proto"
}

// Ensure JSONCodec satisfies encoding.Codec at compile time.
var _ encoding.Codec = JSONCodec{}
