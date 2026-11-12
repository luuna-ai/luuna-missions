package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"
)

// Structured debug records are written only for top-level exceptions.
// Explicit LUUNA_MISSIONS_LOG_LEVEL takes precedence over DEBUG=1, and an
// invalid level falls back to the DEBUG/default rule.
func resolvedLogLevel() string {
	level := os.Getenv("LUUNA_MISSIONS_LOG_LEVEL")
	switch level {
	case "trace", "debug", "info", "warn", "error", "fatal", "silent":
		return level
	}
	if os.Getenv("LUUNA_MISSIONS_DEBUG") == "1" {
		return "debug"
	}
	return "info"
}

func logDebugError(out io.Writer, command string, err error) {
	level := resolvedLogLevel()
	if level != "debug" && level != "trace" {
		return
	}
	hostname, _ := os.Hostname()
	entry := struct {
		Level    int    `json:"level"`
		Time     int64  `json:"time"`
		PID      int    `json:"pid"`
		Hostname string `json:"hostname"`
		Err      struct {
			Type    string `json:"type"`
			Message string `json:"message"`
			Stack   string `json:"stack"`
		} `json:"err"`
		Command string `json:"cmd"`
		Message string `json:"msg"`
	}{Level: 20, Time: time.Now().UnixMilli(), PID: os.Getpid(), Hostname: hostname, Command: command, Message: "command failed"}
	entry.Err.Type = "Error"
	entry.Err.Message = err.Error()
	entry.Err.Stack = fmt.Sprintf("Error: %s", err)
	encoder := json.NewEncoder(out)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(entry) // diagnostics must not replace the original error
}
