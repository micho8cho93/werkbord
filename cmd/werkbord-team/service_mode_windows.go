//go:build windows

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows/svc"
)

func isWindowsService() bool {
	service, err := svc.IsWindowsService()
	return err == nil && service
}

func runWindowsService() error { return svc.Run("dev.werkbord.team", teamWindowsService{}) }

func openWindowsServiceLog(args []string) (*os.File, error) {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == "--data-dir" {
			if !filepath.IsAbs(args[i+1]) {
				return nil, errors.New("Windows service data directory must be absolute")
			}
			return os.OpenFile(filepath.Join(args[i+1], "service.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		}
	}
	return nil, errors.New("Windows service data directory is missing")
}

type teamWindowsService struct{}

func (teamWindowsService) Execute(_ []string, requests <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	logFile, err := openWindowsServiceLog(os.Args[1:])
	if err != nil {
		return true, 1
	}
	defer logFile.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- run(ctx, os.Args[1:], logFile, logFile) }()
	current := svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}
	status <- current
	for {
		select {
		case req, ok := <-requests:
			if !ok {
				cancel()
				if err := <-done; err != nil {
					_, _ = fmt.Fprintln(logFile, "Team service stopped:", err)
					return true, 1
				}
				return false, 0
			}
			switch req.Cmd {
			case svc.Interrogate:
				status <- current
			case svc.Stop, svc.Shutdown:
				current = svc.Status{State: svc.StopPending}
				status <- current
				cancel()
				if err := <-done; err != nil {
					_, _ = fmt.Fprintln(logFile, "Team service stopped:", err)
					return true, 1
				}
				return false, 0
			}
		case err := <-done:
			if err != nil {
				_, _ = fmt.Fprintln(logFile, "Team service stopped:", err)
				return true, 1
			}
			return false, 0
		}
	}
}
