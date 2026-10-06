//go:build darwin && updatertest

package main

// #cgo CFLAGS: -DWB_UPDATER_TEST
import "C"

const updaterTestBuild = true
