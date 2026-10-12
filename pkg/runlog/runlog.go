package runlog

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"
)

// Sets uup opens logs/<prefix>_<timestamp>.log, creating the logs directory if needed, and routes the
// standard logger to it. if stdout is a terminal it also mirrors to stdout so the console keeps behaving as before
func Setup(prefix string) *os.File {
	dir := "logs"
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Printf("[RUNLOG] could not create %s: %v, logging to stdout only", dir, err)
		return nil
	}

	name := fmt.Sprintf("%s_%s.log", prefix, time.Now().Format("20060102_150405"))
	path := filepath.Join(dir, name)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		log.Printf("[RUNLOG] could not open %s: %v, logging to stdout only", path, err)
		return nil
	}

	if mirrorStdout() {
		log.SetOutput(io.MultiWriter(os.Stdout, f))
	} else {
		log.SetOutput(f)
	}
	log.Printf("[RUNLOG] this run's log is being written to %s", path)
	return f
}

// mirrorStdout reports whether log output should also go to stdout: yes when stdout is a character
// device (an interactive terminal), or when ERIDIAN_LOG_STDOUT is set to opt back in under systemd.
func mirrorStdout() bool {
	if os.Getenv("ERIDIAN_LOG_STDOUT") == "1" {
		return true
	}
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}
