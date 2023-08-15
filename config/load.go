package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
)

// LoadConfigFromFlags attempts to read and parse configuration from the node config file path.
func LoadConfigFromFlags(nodeConfigPath, dirPrefix string) (Config, error) {
	configPaths := []string{nodeConfigPath}

	configDir, err := parseConfigDir(nodeConfigPath)
	if err != nil {
		return Config{}, err
	}

	if configDir != "" {
		providerConfigPaths, err := filesInFolder(dirPrefix + configDir)
		if err != nil {
			return Config{}, err
		}
		configPaths = append(configPaths, providerConfigPaths...)
	}

	return ParseConfigs(configPaths)
}

// filesInFolder returns a slice of all file paths in a given folder.
func filesInFolder(folder string) ([]string, error) {
	var files []string
	err := filepath.Walk(folder, func(path string, info os.FileInfo, err error) error {
		if info != nil && !info.IsDir() {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

// parseConfigDir attempts to read the config_dir from the node config file.
func parseConfigDir(nodeConfigPath string) (string, error) {
	var cfg Config
	viper.SetConfigFile(nodeConfigPath)
	if err := viper.ReadInConfig(); err != nil {
		return "", fmt.Errorf("failed to read node config: %w", err)
	}
	if err := viper.Unmarshal(&cfg); err != nil {
		return "", fmt.Errorf("failed to decode node config: %w", err)
	}
	return cfg.ConfigDir, nil
}

// ParseConfig attempts to read and parse configuration from the given file path.
// An error is returned if reading or parsing the config fails.
func ParseConfig(configPath string) (Config, error) {
	return ParseConfigs([]string{configPath})
}

// ParseConfigs attempts to read and parse configuration from the given file paths.
// An error is returned if reading or parsing the configs fails.
func ParseConfigs(configPaths []string) (Config, error) {
	var cfg Config

	viper.AutomaticEnv()
	// Allow nested env vars to be read with underscore separators.
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	// Loop over each config path and merge its values into the previous one
	for _, configPath := range configPaths {
		if configPath == "" {
			return cfg, ErrEmptyConfigPath
		}
		viper.SetConfigFile(configPath)
		if err := viper.MergeInConfig(); err != nil {
			return cfg, fmt.Errorf("failed to read config: %w", err)
		}
	}

	if err := viper.Unmarshal(&cfg); err != nil {
		return cfg, fmt.Errorf("failed to decode config: %w", err)
	}

	cfg.setDefaults()

	return cfg, cfg.Validate()
}
