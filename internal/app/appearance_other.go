//go:build (!darwin && !linux) || server

package app

// setNativeAppearance has no native window chrome to change in the server
// build or off macOS and Linux; the web page themes itself.
func setNativeAppearance(string) {}
