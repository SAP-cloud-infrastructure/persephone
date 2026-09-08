package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Load loads the config from the given path.
func Load(path string) (PersephoneConfig, error) {
	var webhookConfig PersephoneConfig
	configBytes, err := os.ReadFile(path)
	if err != nil {
		return PersephoneConfig{}, fmt.Errorf("failed to read config file %q: %w", path, err)
	}

	if err := yaml.Unmarshal(configBytes, &webhookConfig); err != nil {
		return PersephoneConfig{}, fmt.Errorf("failed to unmarshal config file %q: %w", path, err)
	}

	return webhookConfig, nil
}
