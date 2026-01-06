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
	"errors"
	"fmt"
	"sync"

	"github.com/spf13/viper"
)

var (
	cfg        Config
	once       sync.Once
	initErr    error
	configured bool
	mu         sync.RWMutex
)

// Config holds all configuration for the webhook.
type Config struct {
	Cache          CacheConfig   `yaml:"cache" mapstructure:"cache"`
	IngressClasses []string      `yaml:"ingressClasses" mapstructure:"ingressClasses"`
	Webhook        WebhookConfig `yaml:"webhook" mapstructure:"webhook"`
}

// CacheConfig holds cache-specific configuration.
type CacheConfig struct {
	CleanUpIntervalSecond int `yaml:"cleanUpIntervalSecond" mapstructure:"cleanUpIntervalSecond"`
	EntryTTLSecond        int `yaml:"entryTtlSecond" mapstructure:"entryTtlSecond"`
}

// WebhookConfig holds webhook server configuration.
type WebhookConfig struct {
	Port        int    `yaml:"port" mapstructure:"port"`
	TLSCertFile string `yaml:"tlsCertFile" mapstructure:"tlsCertFile"`
	TLSKeyFile  string `yaml:"tlsKeyFile" mapstructure:"tlsKeyFile"`
}

// Validate validates the configuration and returns an error if invalid.
func (c *Config) Validate() error {
	var errs []error

	if c.Cache.CleanUpIntervalSecond <= 0 {
		errs = append(errs, errors.New("cache.cleanUpIntervalSecond must be positive"))
	}

	if c.Cache.EntryTTLSecond < 0 {
		errs = append(errs, errors.New("cache.entryTtlSecond must be non-negative"))
	}

	if len(c.IngressClasses) == 0 {
		errs = append(errs, errors.New("ingressClasses must not be empty"))
	}

	if c.Webhook.Port <= 0 || c.Webhook.Port > 65535 {
		errs = append(errs, errors.New("webhook.port must be between 1 and 65535"))
	}

	if c.Webhook.TLSCertFile == "" {
		errs = append(errs, errors.New("webhook.tlsCertFile must not be empty"))
	}

	if c.Webhook.TLSKeyFile == "" {
		errs = append(errs, errors.New("webhook.tlsKeyFile must not be empty"))
	}

	if len(errs) > 0 {
		return fmt.Errorf("configuration validation failed: %w", errors.Join(errs...))
	}

	return nil
}

// GetConfig returns the current configuration.
// Panics if InitializeConfig has not been called successfully.
func GetConfig() Config {
	mu.RLock()
	defer mu.RUnlock()

	if !configured {
		panic("config: GetConfig called before successful InitializeConfig")
	}

	return cfg
}

// IsConfigured returns true if the configuration has been successfully initialized.
func IsConfigured() bool {
	mu.RLock()
	defer mu.RUnlock()

	return configured
}

// InitializeConfig reads and validates configuration from the specified file path.
// It is safe to call multiple times, but only the first call will have effect.
func InitializeConfig(configFilePath string) error {
	once.Do(func() {
		initErr = loadConfig(configFilePath)
		if initErr == nil {
			mu.Lock()
			configured = true
			mu.Unlock()
		}
	})

	return initErr
}

// loadConfig reads and validates the configuration file.
func loadConfig(configFilePath string) error {
	viper.SetConfigFile(configFilePath)

	if err := viper.ReadInConfig(); err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	var newCfg Config
	if err := viper.UnmarshalExact(&newCfg); err != nil {
		return fmt.Errorf("failed to unmarshal config: %w", err)
	}

	if err := newCfg.Validate(); err != nil {
		return err
	}

	mu.Lock()
	cfg = newCfg
	mu.Unlock()

	return nil
}

// ResetForTesting resets the configuration state.
// This is intended for use in tests only.
func ResetForTesting() {
	mu.Lock()
	defer mu.Unlock()

	cfg = Config{}
	once = sync.Once{}
	initErr = nil
	configured = false
}
