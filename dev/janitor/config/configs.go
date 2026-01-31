// package config controls matrixOS development config files loading and config
// params reading.
package config

type SingleConfigValue struct {
	Item string
}

type MultipleConfigValues struct {
	Items []string
}

type IConfig interface {
	// Load loads the associated config file or source.
	Load() error

	// GetItem retrieves the single config value associated to the provided config key.
	// Config keys can be of type: category.name. It also handles the expansion
	// of predefined env variables in the config value. e.g. MATRIXOS_DEV_DIR.
	GetItem(key string) (SingleConfigValue, error)

	// GetItems retrieves the config values associated to the provided config key.
	// Config keys can be of type: category.name. It also handles the expansion
	// of predefined env variables in the config value. e.g. MATRIXOS_DEV_DIR.
	GetItems(key string) (MultipleConfigValues, error)
}
