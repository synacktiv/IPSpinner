package utils

// Returns a random key in the given map
func RandomKeyOfMap[T any](m map[string]T) string {
	keys := make([]string, 0, len(m))

	for key := range m {
		keys = append(keys, key)
	}

	if len(keys) == 0 {
		return ""
	}

	randomIndex := generateSecureRandomInt(len(keys))

	return keys[randomIndex]
}

// Returns the value associated to the key in the given map if it exists, or the defaultValue otherwise
func GetOrDefault[K comparable, V any](m map[K]V, key K, defaultValue V) V {
	if val, ok := m[key]; ok {
		return val
	}

	return defaultValue
}
