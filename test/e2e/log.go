package e2e

import (
	"fmt"
	"time"

	"github.com/onsi/ginkgo/v2"
)

// Provide some wrapper funcs for formatted logging using the GinkgoWriter.
// Other logs would only appear in case of failing tests.
// If we need more funcs, we might want to investigate if / how we can use
// that writer with existing logging frameworks.

const (
	INFO = "INFO"
	WARN = "WARN"
)

func logInfoln(arg string) {
	log(INFO, fmt.Sprintf("%s\n", arg))
}

func logInfof(format string, args ...interface{}) {
	log(INFO, fmt.Sprintf(format, args...))
}

func logWarnln(arg string) {
	log(WARN, fmt.Sprintf("%s\n", arg))
}

func logWarnf(format string, args ...interface{}) {
	log(WARN, fmt.Sprintf(format, args...))
}

func log(prefix, arg string) {
	now := time.Now().Format(time.RFC3339)
	msg := fmt.Sprintf("%s: [%s] %s", now, prefix, arg)
	_, _ = ginkgo.GinkgoWriter.Write([]byte(msg))
}
