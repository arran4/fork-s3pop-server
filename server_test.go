package main

import (
	"encoding/json"
	"testing"
)

func TestConfigUnmarshal(t *testing.T) {
	jsonData := []byte(`{"s3Endpoint": "http://localhost:9000", "s3ForcePathStyle": true}`)
	config := new(ServerConfig)
	err := json.Unmarshal(jsonData, config)
	if err != nil {
		t.Fatalf("Failed to unmarshal config: %v", err)
	}

	if config.S3Endpoint != "http://localhost:9000" {
		t.Errorf("Expected S3Endpoint to be 'http://localhost:9000', got '%v'", config.S3Endpoint)
	}

	if config.S3ForcePathStyle == nil || *config.S3ForcePathStyle != true {
		t.Errorf("Expected S3ForcePathStyle to be true, got %v", config.S3ForcePathStyle)
	}
}
