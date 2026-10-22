//go:build linux

package main

import "fmt"

func renderFloatingFixture(path string) error {
	return fmt.Errorf("native fixture rendering requires Windows")
}

func showFloating(data string) {}

func (a *App) startFloating() func() { return func() {} }

func runVisualBenchmark(path string) error {
	return fmt.Errorf("native renderer benchmark requires Windows")
}
