package config

import (
	"fmt"
	"os"

	"github.com/go-playground/validator/v10"
	"github.com/rajkumaar23/firefly-bridge/internal/institution"
	"gopkg.in/yaml.v2"
)

type FireflyConfig struct {
	BaseURL string `yaml:"base_url" validate:"http_url"`
	Token   string `yaml:"token" validate:"jwt"`
}

type Config struct {
	Firefly         FireflyConfig             `yaml:"firefly" validate:"required"`
	BrowserExecPath string                    `yaml:"browser_exec_path" validate:"required"`
	Institutions    []institution.Institution `yaml:"institutions" validate:"min=1,dive"`
}

func (c *Config) GetDownloadCount() uint8 {
	var sum uint8 = 0
	for _, i := range c.Institutions {
		sum += i.Downloads
	}
	return sum
}

func (c *Config) Validate() error {
	validate := validator.New(validator.WithRequiredStructEnabled())

	if err := validate.Struct(c); err != nil {
		return err
	}
	return nil
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

	err = cfg.Validate()
	if err != nil {
		return nil, fmt.Errorf("failed to validate config: %w", err)
	}

	return &cfg, nil
}
