//go:build !linux

package app

import "errors"

// Smart open is implemented for Linux first. Elsewhere discovery finds nothing
// and the caller falls back to the OS URL handler.
func findPWALauncher(string) (pwaLauncher, bool) { return pwaLauncher{}, false }

func launchPWALauncher(pwaLauncher) error {
	return errors.New("smart open is not implemented on this platform")
}
