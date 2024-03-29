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
	"github.com/spf13/viper"
)

var config Config

type Config struct {
	Cache          Cache    `yaml:"cache"`
	IngressClasses []string `yaml:"ingressClasses"`
	Webhook        Webhook  `yaml:"webhook"`
}

type Cache struct {
	CleanUpIntervalSecond int `yaml:"cleanUpIntervalSecond"`
	EntryTtlSecond        int `yaml:"entryTtlSecond"`
}

type Webhook struct {
	Port        int    `yaml:"port"`
	TLSCertFile string `yaml:"tlsCertFile"`
	TLSKeyFile  string `yaml:"tlsKeyFile"`
}

func GetConfig() Config {
	return config
}

func InitializeConfig(configFilePath string) error {
	viper.SetConfigFile(configFilePath)

	err := viper.ReadInConfig()
	if err != nil {
		return err
	}

	if err := viper.UnmarshalExact(&config); err != nil {
		return err
	}

	return nil
}
