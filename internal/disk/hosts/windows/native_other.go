//go:build !windows

package windowsdisk

func native(request) error { return ErrHost }
