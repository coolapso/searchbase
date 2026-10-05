package scraper

import (
	"context"
	"errors"
	"net/http"
)

// FetchError contains only a fixed, URL-free category approved for clients and telemetry.
type FetchError string

func (e FetchError) Error() string { return string(e) }

func safeWorkerError(value string) FetchError {
	switch value {
	case "not_found", "forbidden", "robots_denied", "rate_limited", "timeout", "unreachable", "upstream_error", "extraction_failed":
		return FetchError(value)
	default:
		return FetchError("extraction_failed")
	}
}

// FetchFailure returns the public category and REST status for a failed fetch.
func FetchFailure(err error) (string, int) {
	category := "extraction_failed"
	var failure FetchError
	if errors.As(err, &failure) {
		category = string(safeWorkerError(string(failure)))
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		category = "timeout"
	}
	status := http.StatusInternalServerError
	switch category {
	case "not_found":
		status = http.StatusNotFound
	case "forbidden", "robots_denied":
		status = http.StatusForbidden
	case "rate_limited":
		status = http.StatusTooManyRequests
	case "timeout":
		status = http.StatusGatewayTimeout
	case "unreachable", "upstream_error":
		status = http.StatusBadGateway
	}
	return category, status
}
