//go:build bindings

package main

// generatingBindings is true when `wails build` runs the program only to
// extract the JS bindings; it must not touch user data then.
const generatingBindings = true
