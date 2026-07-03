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

	forcePathStyle := (*bool)(config.S3ForcePathStyle)
	if forcePathStyle == nil || *forcePathStyle != true {
		t.Errorf("Expected S3ForcePathStyle to be true, got %v", config.S3ForcePathStyle)
	}
}

func TestEnvConfig(t *testing.T) {
	// Set environment variables
	t.Setenv("S3POP_PORT", "8110")
	t.Setenv("S3POP_S3_BUCKET", "test-bucket")
	t.Setenv("S3POP_S3_ENDPOINT", "http://localhost:9001")
	t.Setenv("S3POP_S3_FORCE_PATH_STYLE", "true")
	t.Setenv("S3POP_CONFIG", "non-existent-file.json") // Ensure it doesn't fail if config file doesn't exist

	config := loadConfig()

	if config.Port != 8110 {
		t.Errorf("Expected Port to be 8110, got %d", config.Port)
	}

	if config.S3Bucket != "test-bucket" {
		t.Errorf("Expected S3Bucket to be 'test-bucket', got '%s'", config.S3Bucket)
	}

	if config.S3Endpoint != "http://localhost:9001" {
		t.Errorf("Expected S3Endpoint to be 'http://localhost:9001', got '%v'", config.S3Endpoint)
	}

	forcePathStyle := (*bool)(config.S3ForcePathStyle)
	if forcePathStyle == nil || *forcePathStyle != true {
		t.Errorf("Expected S3ForcePathStyle to be true, got %v", config.S3ForcePathStyle)
	}
}
