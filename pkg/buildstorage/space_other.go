//go:build !darwin

package buildstorage

import "fmt"

func measure(string) (uint64, string, error) {
	return 0, "", fmt.Errorf("macOS storage discovery requires a macOS host")
}
