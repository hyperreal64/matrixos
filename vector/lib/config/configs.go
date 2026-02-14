// package config controls matrixOS development config files loading and config
// params reading.
package config

type IConfig interface {
	// Load loads the associated config file or source.
	Load() error

	// GetItem retrieves the single config value associated to the provided config key.
	// Config keys can be of type: category.name.
	GetItem(key string) (string, error)

	// GetBool retrieves the single config value associated to the provided config key
	// and casts it to a bool value. This is a shortcut function for config values that
	// are strictly boolean.
	GetBool(key string) (bool, error)

	// GetItems retrieves the config values associated to the provided config key.
	// Config keys can be of type: category.name.
	GetItems(key string) ([]string, error)
}
