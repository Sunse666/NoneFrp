package protocol

import (
	"encoding/json"
	"testing"
)

func TestEncodeDecodeRoundTrip(t *testing.T) {
	raw, err := Encode(TypeRegister, Register{DeviceID: "dev-1", LocalPort: 25565})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	env, err := Decode(raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.V != Version {
		t.Fatalf("version = %d, want %d", env.V, Version)
	}
	if env.Type != TypeRegister {
		t.Fatalf("type = %q, want %q", env.Type, TypeRegister)
	}

	got, err := As[Register](env)
	if err != nil {
		t.Fatalf("as: %v", err)
	}
	if got.DeviceID != "dev-1" || got.LocalPort != 25565 {
		t.Fatalf("payload = %+v", got)
	}
}

func TestDecodeRejectsMissingType(t *testing.T) {
	if _, err := Decode([]byte(`{"v":1}`)); err == nil {
		t.Fatal("expected an error for a missing type")
	}
}

func TestDecodeToleratesUnknownTypeAndFields(t *testing.T) {
	// A newer peer may send message types and fields this build does not know.
	env, err := Decode([]byte(`{"v":1,"type":"future_thing","body":{"kept":"ignored"}}`))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Type != "future_thing" {
		t.Fatalf("type = %q", env.Type)
	}

	raw, err := json.Marshal(map[string]any{"device_id": "d", "unknown": true})
	if err != nil {
		t.Fatal(err)
	}
	var reg Register
	if err := json.Unmarshal(raw, &reg); err != nil {
		t.Fatalf("tolerant decode: %v", err)
	}
	if reg.DeviceID != "d" {
		t.Fatalf("device id = %q", reg.DeviceID)
	}
}

func TestAsEmptyBodyReturnsZeroValue(t *testing.T) {
	got, err := As[Unregister](Envelope{Type: TypeUnregister})
	if err != nil {
		t.Fatalf("as: %v", err)
	}
	if got.Reason != "" {
		t.Fatalf("reason = %q", got.Reason)
	}
}

func TestEncodeErrorBody(t *testing.T) {
	raw, err := Encode(TypeError, ErrorBody{Code: ErrUnauthorized, Message: "bad token"})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	env, err := Decode(raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	body, err := As[ErrorBody](env)
	if err != nil {
		t.Fatalf("as: %v", err)
	}
	if body.Code != ErrUnauthorized {
		t.Fatalf("code = %q", body.Code)
	}
}
