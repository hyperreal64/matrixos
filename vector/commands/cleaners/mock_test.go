package cleaners

import (
	"fmt"
	"matrixos/lib/config"
)

// MockConfig is a mock implementation of the IConfig interface for testing purposes.
type MockConfig struct {
	values map[string]interface{}
}

func (m *MockConfig) Load() error {
	return nil
}

func (m *MockConfig) GetItem(key string) (config.SingleConfigValue, error) {
	if val, ok := m.values[key]; ok {
		if str, ok := val.(string); ok {
			return config.SingleConfigValue{Item: str}, nil
		}
	}
	return config.SingleConfigValue{}, fmt.Errorf("item with key '%s' not found", key)
}

func (m *MockConfig) GetItems(key string) (config.MultipleConfigValues, error) {
	if val, ok := m.values[key]; ok {
		if items, ok := val.([]string); ok {
			return config.MultipleConfigValues{Items: items}, nil
		}
	}
	return config.MultipleConfigValues{}, fmt.Errorf("items with key '%s' not found", key)
}
