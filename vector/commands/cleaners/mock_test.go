package cleaners

import (
	"fmt"
)

// MockConfig is a mock implementation of the IConfig interface for testing purposes.
type MockConfig struct {
	values map[string]interface{}
}

func (m *MockConfig) Load() error {
	return nil
}

func (m *MockConfig) GetItem(key string) (string, error) {
	if val, ok := m.values[key]; ok {
		if str, ok := val.(string); ok {
			return str, nil
		}
	}
	return "", fmt.Errorf("item with key '%s' not found", key)
}

func (m *MockConfig) GetBool(key string) (bool, error) {
	if val, ok := m.values[key]; ok {
		if b, ok := val.(bool); ok {
			return b, nil
		}
	}
	return false, fmt.Errorf("bool with key '%s' not found", key)
}

func (m *MockConfig) GetItems(key string) ([]string, error) {
	if val, ok := m.values[key]; ok {
		if items, ok := val.([]string); ok {
			return items, nil
		}
	}
	return nil, fmt.Errorf("items with key '%s' not found", key)
}
