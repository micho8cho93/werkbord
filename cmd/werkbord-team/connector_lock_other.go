//go:build !darwin && !linux

package main

import "errors"

func lockConnector(string) (func(), error) {
	return nil, errors.New("the user connector is supported on macOS and Linux")
}
