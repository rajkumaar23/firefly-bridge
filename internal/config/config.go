package config

import (
	"fmt"
	"os"
	"reflect"

	"github.com/go-playground/validator/v10"
	"github.com/rajkumaar23/firefly-bridge/internal/chromedp"
	"github.com/rajkumaar23/firefly-bridge/internal/institution"
	"gopkg.in/yaml.v3"
)

type FireflyConfig struct {
	Host  string `yaml:"host" validate:"http_url"`
	Token string `yaml:"token" validate:"jwt"`
}

type Config struct {
	Firefly         FireflyConfig             `yaml:"firefly" validate:"required"`
	BrowserExecPath string                    `yaml:"browser_exec_path" validate:"file"`
	Institutions    []institution.Institution `yaml:"institutions" validate:"min=1,dive"`
}

func (c *Config) Validate() error {
	validate := validator.New(validator.WithRequiredStructEnabled())

	validate.RegisterCustomTypeFunc(func(field reflect.Value) interface{} {
		if step, ok := field.Interface().(chromedp.BrowserStep); ok {
			return step.Step
		}
		return nil
	}, chromedp.BrowserStep{})

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
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal yaml: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("failed to validate: %w", err)
	}

	return &cfg, nil
}
