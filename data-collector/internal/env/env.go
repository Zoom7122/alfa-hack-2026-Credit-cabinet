// Package env читает настройки бинарей data-collector из переменных окружения.
package env

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// String — значение переменной или def, если она не задана.
func String(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// Duration — длительность вида 5s, 500ms.
func Duration(key string, def time.Duration) (time.Duration, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return d, nil
}

// PositiveInt — целое больше нуля.
func PositiveInt(key string, def int) (int, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	if n <= 0 {
		return 0, fmt.Errorf("%s: должно быть больше нуля, задано %d", key, n)
	}
	return n, nil
}
