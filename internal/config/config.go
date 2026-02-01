package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v2"
)

type FireflyConfig struct {
	BaseURL string `yaml:"base_url"`
	Token   string `yaml:"token"`
}

type Config struct {
	Firefly FireflyConfig `yaml:"firefly"`
}

func NewConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	var cfg Config
	err = yaml.Unmarshal(data, &cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal yaml: %w", err)
	}

	return &cfg, nil
}
