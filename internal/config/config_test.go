/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config",
			config: Config{
				Cache: CacheConfig{
					CleanUpIntervalSecond: 30,
					EntryTTLSecond:        10,
				},
				IngressClasses: []string{"public", "private"},
				Webhook: WebhookConfig{
					Port:        8443,
					TLSCertFile: "/path/to/cert",
					TLSKeyFile:  "/path/to/key",
				},
			},
			wantErr: false,
		},
		{
			name: "zero cleanup interval",
			config: Config{
				Cache: CacheConfig{
					CleanUpIntervalSecond: 0,
					EntryTTLSecond:        10,
				},
				IngressClasses: []string{"public"},
				Webhook: WebhookConfig{
					Port:        8443,
					TLSCertFile: "/path/to/cert",
					TLSKeyFile:  "/path/to/key",
				},
			},
			wantErr: true,
			errMsg:  "cleanUpIntervalSecond must be positive",
		},
		{
			name: "negative TTL",
			config: Config{
				Cache: CacheConfig{
					CleanUpIntervalSecond: 30,
					EntryTTLSecond:        -1,
				},
				IngressClasses: []string{"public"},
				Webhook: WebhookConfig{
					Port:        8443,
					TLSCertFile: "/path/to/cert",
					TLSKeyFile:  "/path/to/key",
				},
			},
			wantErr: true,
			errMsg:  "entryTtlSecond must be non-negative",
		},
		{
			name: "empty ingress classes",
			config: Config{
				Cache: CacheConfig{
					CleanUpIntervalSecond: 30,
					EntryTTLSecond:        10,
				},
				IngressClasses: []string{},
				Webhook: WebhookConfig{
					Port:        8443,
					TLSCertFile: "/path/to/cert",
					TLSKeyFile:  "/path/to/key",
				},
			},
			wantErr: true,
			errMsg:  "ingressClasses must not be empty",
		},
		{
			name: "invalid port - zero",
			config: Config{
				Cache: CacheConfig{
					CleanUpIntervalSecond: 30,
					EntryTTLSecond:        10,
				},
				IngressClasses: []string{"public"},
				Webhook: WebhookConfig{
					Port:        0,
					TLSCertFile: "/path/to/cert",
					TLSKeyFile:  "/path/to/key",
				},
			},
			wantErr: true,
			errMsg:  "webhook.port must be between 1 and 65535",
		},
		{
			name: "invalid port - too high",
			config: Config{
				Cache: CacheConfig{
					CleanUpIntervalSecond: 30,
					EntryTTLSecond:        10,
				},
				IngressClasses: []string{"public"},
				Webhook: WebhookConfig{
					Port:        65536,
					TLSCertFile: "/path/to/cert",
					TLSKeyFile:  "/path/to/key",
				},
			},
			wantErr: true,
			errMsg:  "webhook.port must be between 1 and 65535",
		},
		{
			name: "empty TLS cert file",
			config: Config{
				Cache: CacheConfig{
					CleanUpIntervalSecond: 30,
					EntryTTLSecond:        10,
				},
				IngressClasses: []string{"public"},
				Webhook: WebhookConfig{
					Port:        8443,
					TLSCertFile: "",
					TLSKeyFile:  "/path/to/key",
				},
			},
			wantErr: true,
			errMsg:  "tlsCertFile must not be empty",
		},
		{
			name: "empty TLS key file",
			config: Config{
				Cache: CacheConfig{
					CleanUpIntervalSecond: 30,
					EntryTTLSecond:        10,
				},
				IngressClasses: []string{"public"},
				Webhook: WebhookConfig{
					Port:        8443,
					TLSCertFile: "/path/to/cert",
					TLSKeyFile:  "",
				},
			},
			wantErr: true,
			errMsg:  "tlsKeyFile must not be empty",
		},
		{
			name: "zero TTL is valid (persistent entries)",
			config: Config{
				Cache: CacheConfig{
					CleanUpIntervalSecond: 30,
					EntryTTLSecond:        0,
				},
				IngressClasses: []string{"public"},
				Webhook: WebhookConfig{
					Port:        8443,
					TLSCertFile: "/path/to/cert",
					TLSKeyFile:  "/path/to/key",
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestInitializeConfig(t *testing.T) {
	// Create a temporary config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	configContent := `
cache:
  cleanUpIntervalSecond: 30
  entryTtlSecond: 10
ingressClasses:
  - "public"
  - "private"
webhook:
  port: 8443
  tlsCertFile: "/etc/certs/tls.crt"
  tlsKeyFile: "/etc/certs/tls.key"
`
	err := os.WriteFile(configPath, []byte(configContent), 0644)
	require.NoError(t, err)

	// Reset config state
	ResetForTesting()

	// Test initialization
	err = InitializeConfig(configPath)
	assert.NoError(t, err)

	// Verify config is loaded
	assert.True(t, IsConfigured())

	cfg := GetConfig()
	assert.Equal(t, 30, cfg.Cache.CleanUpIntervalSecond)
	assert.Equal(t, 10, cfg.Cache.EntryTTLSecond)
	assert.Equal(t, []string{"public", "private"}, cfg.IngressClasses)
	assert.Equal(t, 8443, cfg.Webhook.Port)
	assert.Equal(t, "/etc/certs/tls.crt", cfg.Webhook.TLSCertFile)
	assert.Equal(t, "/etc/certs/tls.key", cfg.Webhook.TLSKeyFile)
}

func TestInitializeConfig_FileNotFound(t *testing.T) {
	ResetForTesting()

	err := InitializeConfig("/nonexistent/path/config.yaml")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read config file")
}

func TestInitializeConfig_InvalidConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// Config with invalid port
	configContent := `
cache:
  cleanUpIntervalSecond: 30
  entryTtlSecond: 10
ingressClasses:
  - "public"
webhook:
  port: 0
  tlsCertFile: "/etc/certs/tls.crt"
  tlsKeyFile: "/etc/certs/tls.key"
`
	err := os.WriteFile(configPath, []byte(configContent), 0644)
	require.NoError(t, err)

	ResetForTesting()

	err = InitializeConfig(configPath)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "webhook.port must be between 1 and 65535")
}

func TestGetConfig_PanicsIfNotInitialized(t *testing.T) {
	ResetForTesting()

	assert.Panics(t, func() {
		GetConfig()
	})
}

func TestIsConfigured(t *testing.T) {
	ResetForTesting()

	assert.False(t, IsConfigured())
}

func TestResetForTesting(t *testing.T) {
	// Create a temporary config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	configContent := `
cache:
  cleanUpIntervalSecond: 30
  entryTtlSecond: 10
ingressClasses:
  - "public"
webhook:
  port: 8443
  tlsCertFile: "/etc/certs/tls.crt"
  tlsKeyFile: "/etc/certs/tls.key"
`
	err := os.WriteFile(configPath, []byte(configContent), 0644)
	require.NoError(t, err)

	ResetForTesting()
	err = InitializeConfig(configPath)
	require.NoError(t, err)
	assert.True(t, IsConfigured())

	ResetForTesting()
	assert.False(t, IsConfigured())
}

