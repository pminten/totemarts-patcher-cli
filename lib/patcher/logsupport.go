package patcher

import (
	"context"
	"log"
)

// This type is desired by the linter, to avoid conflicts in context keys.
type typeVerbose string

const keyVerbose typeVerbose = "verbose"

// SetVerbose sets the verbose flag for logging.
func SetVerbose(ctx context.Context, verbose bool) context.Context {
	return context.WithValue(ctx, keyVerbose, verbose)
}

// LogVerbose logs a message only if the verbose flag is set.
func LogVerbose(ctx context.Context, format string, args ...any) {
	verbose, ok := ctx.Value(keyVerbose).(bool)
	if ok && verbose {
		log.Printf(format, args...)
	}
}
