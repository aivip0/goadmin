package main

import (
	"compress/gzip"
	"io"
)

func newGzipReader(reader io.Reader) (*gzip.Reader, error) { return gzip.NewReader(reader) }
