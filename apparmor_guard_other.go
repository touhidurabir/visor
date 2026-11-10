//go:build !linux || server

package main

// guardWebKitSandbox: only Linux's AppArmor can stop the window opening.
func guardWebKitSandbox(string) {}
