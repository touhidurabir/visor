//go:build !server

package main

// guardEnvironment has nothing to check in the desktop app.
func guardEnvironment() {}
