package test

import (
	"encoding/json"
	"testing"

	"github.com/joshua-temple/nexus"
)

// TestContextUnmarshal tests the Unmarshal method
func TestContextUnmarshal(t *testing.T) {
	type TestPayload struct {
		Name  string `json:"name"`
		Value int    `json:"value"`
	}

	payload := TestPayload{Name: "test", Value: 42}
	data, _ := json.Marshal(payload)

	env := &nexus.Envelope{
		Payload: data,
	}

	ctx := &nexus.Context{
		Envelope: env,
	}

	var result TestPayload
	if err := ctx.Unmarshal(&result); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if result.Name != "test" || result.Value != 42 {
		t.Errorf("Unexpected unmarshal result: %+v", result)
	}
}
