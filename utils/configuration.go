package utils

import (
	"errors"
	"fmt"
	"os"
	"path"

	"github.com/joho/godotenv"
	"go.yaml.in/yaml/v4"
)

var (
	// this is the map of all environment variables that were set via .env file
	dotEnvValues = map[string]string{}

	concurrency = true
)

func ConcurrencyIsEnabled() bool {
	return concurrency
}

func SetConcurrencyEnabled(enabled bool) {
	concurrency = enabled
}

func getEnvValue(envName, defaultValue string) (val string, dotEnvSource bool) {
	val = os.Getenv(envName)
	if val == "" {
		return defaultValue, false
	}

	_, exists := dotEnvValues[envName]
	return val, exists
}

func LoadEnvFile(envFilePath string) error {
	// sometimes i mixed up yaml and .env files due to copy paste
	// so let's give users a better error message if they also try
	// to accidentally load a yaml file here instead of an .env file
	content, err := os.ReadFile(envFilePath)
	if err != nil {
		return fmt.Errorf("unable to load env file: %s", envFilePath)
	}

	var yamlCheck map[string]any
	ext := path.Ext(envFilePath)
	err = yaml.Unmarshal(content, &yamlCheck)

	// double check, maybe the decoder failed due to syntax error but its still a yaml file
	if err == nil || ext == ".yaml" || ext == ".yml" {
		return fmt.Errorf("file %q appears to be a YAML file, but an .env file is required (KEY=VALUE format)", envFilePath)
	}

	file, err := os.Open(envFilePath)
	if err != nil {
		return err
	}
	defer file.Close()

	dotEnvValues, err := godotenv.Parse(file)
	if err != nil {
		return err
	}

	for key, value := range dotEnvValues {
		_, exists := os.LookupEnv(key)

		// shell environment variables have precedence over .env file values
		if !exists {
			_ = os.Setenv(key, value)
		}
	}

	return err
}

func LoadConfig(configFile string) (*Config, error) {
	var (
		config = Config{
			values:  map[string]any{},
			Secrets: map[string]string{},
			Inputs:  map[string]any{},
			Env:     map[string]string{},
		}
	)

	err := config.LoadFile(configFile)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, errors.Join(fmt.Errorf("path: %s", configFile), err)
		}
	}

	// 1. Initialize maps with Config File values (Priority 4 - Lowest in this function)
	envMap := make(map[string]string)
	secretMap := make(map[string]string)
	inputMap := make(map[string]any)

	// Load Env from Config
	env := config.GetAll("env")
	for k, v := range env {
		if v == nil {
			continue
		}
		envMap[k] = fmt.Sprintf("%v", v)
	}

	secrets := config.GetAll("secrets")
	for k, v := range secrets {
		if v == nil {
			continue
		}
		secretMap[k] = fmt.Sprintf("%v", v)
	}

	inputs := config.GetAll("inputs")
	for k, v := range inputs {
		if v == nil {
			continue
		}
		inputMap[k] = fmt.Sprintf("%v", v)
	}

	config.Env = envMap
	config.Secrets = secretMap
	config.Inputs = inputMap

	return &config, nil
}
