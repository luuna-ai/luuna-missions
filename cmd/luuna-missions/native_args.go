package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

const (
	maxNativeArgsBytes = 1 << 20
	maxNativeArgCount  = 128
	maxNativeArgBytes  = 256 << 10
)

// readNativeArgs decodes the private argv request used by the Rust runtime.
// The protocol keeps potentially sensitive values out of process listings;
// ordinary CLI calls continue to use normal operating-system arguments.
func readNativeArgs(input io.Reader) ([]string, error) {
	data, err := io.ReadAll(io.LimitReader(input, maxNativeArgsBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read request: %w", err)
	}
	if len(data) > maxNativeArgsBytes {
		return nil, errors.New("request exceeds size limit")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	var args []string
	if err := decoder.Decode(&args); err != nil || args == nil {
		return nil, errors.New("request must be a JSON string array")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, errors.New("request contains trailing data")
	}
	if len(args) == 0 || len(args) > maxNativeArgCount {
		return nil, errors.New("request has invalid argument count")
	}
	for _, arg := range args {
		if len(arg) > maxNativeArgBytes || strings.IndexByte(arg, 0) >= 0 {
			return nil, errors.New("request contains an invalid argument")
		}
	}
	return args, nil
}
