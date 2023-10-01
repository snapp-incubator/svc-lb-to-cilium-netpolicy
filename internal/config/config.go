/*
Copyright 2023.

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

	"github.com/spf13/viper"
)

var config Config

type Config struct {
	Controller Controller `yaml:"controller"`
}

type Controller struct {
	Exclude Exclude `yaml:"exclude"`
}

type Exclude struct {
	NamespaceSelector NamespaceSelector `yaml:"namespaceSelector"`
}

type NamespaceSelector struct {
	MatchLabels []MatchLabel `yaml:"matchLabels"`
	MatchNames  []string     `yaml:"matchNames"`
}

type MatchLabel struct {
	Labels      map[string]string `yaml:"labels"`
	MatcherName string            `yaml:"matcherName"`
}

func GetConfig() *Config {
	return &config
}

func InitializeConfig(configFilePath string) error {
	viper.SetConfigFile(configFilePath)

	err := viper.ReadInConfig()
	if err != nil {
		return err
	}

	// TODO: add hot-reload support
	// viper.WatchConfig()
	// viper.OnConfigChange(
	// 	func(e fsnotify.Event) {
	// 		if e.Op == fsnotify.Write {
	// 			newConfig := Config{}
	// 			if err := viper.Unmarshal(&newConfig); err != nil {
	// 				log.Fatal(err)
	// 			}
	// 			config = &newConfig
	// 			log.Printf("Change occurs %#v", config)
	// 		}
	// 	},
	// )

	if err := viper.UnmarshalExact(&config); err != nil {
		return err
	}

	return validateConfig()
}

func validateConfig() error {
	if config.Controller.Exclude.NamespaceSelector.MatchNames == nil {
		return errors.New("The matchNames field value is not defined in the config file")
	}

	if config.Controller.Exclude.NamespaceSelector.MatchLabels == nil {
		return errors.New("The matchLabels field value is not defined in the config file")
	}

	return nil
}
