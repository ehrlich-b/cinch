package server

import "errors"

var (
	ErrWorkerNotFound    = errors.New("worker not found")
	ErrNoWorkerAvailable = errors.New("no worker available")
)
