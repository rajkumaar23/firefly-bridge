package config

import (
	"fmt"
	"os"

	"github.com/rajkumaar23/firefly-bridge/internal/institution"
	"gopkg.in/yaml.v2"
)

type FireflyConfig struct {
	BaseURL      string                    `yaml:"base_url"`
	Token        string                    `yaml:"token"`
	Institutions []institution.Institution `yaml:"institutions"`
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
