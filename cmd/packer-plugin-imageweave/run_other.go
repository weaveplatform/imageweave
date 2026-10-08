//go:build !darwin || !arm64

package main

func runMain(work func()) { work() }
