//go:build !darwin || server

package app

// setNativeAppearance has no native window chrome to change in the server
// build or off macOS; the web page themes itself.
func setNativeAppearance(string) {}
