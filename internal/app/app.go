// Package app wires the dependencies every command needs.
package app

import (
	"io"
	"time"
)

// App carries injected dependencies. Later tasks add Store, Prober, Identity, Git.
type App struct {
	Stdout io.Writer
	Stderr io.Writer
	Now    func() time.Time
}

func (a *App) now() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now().UTC()
}

// Clock returns the current time via the injected clock.
func (a *App) Clock() time.Time { return a.now() }
