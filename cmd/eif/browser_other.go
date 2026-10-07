//go:build !windows

package main

func openBrowser(_ string) error {
	return nil
}
