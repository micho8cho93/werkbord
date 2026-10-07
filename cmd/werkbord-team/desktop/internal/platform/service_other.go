//go:build !darwin

package platform

import (
	"context"
	"errors"
)

func AuthorizeService(context.Context, string) error {
	return errors.New("desktop service installation is supported on macOS; Linux and Windows installers are not available yet")
}
func PrivilegedService(string, string) error { return errors.New("this native installer is for macOS") }
func SetupRunner(context.Context) error {
	return errors.New("install the free Werkbord app using its supported installer")
}
