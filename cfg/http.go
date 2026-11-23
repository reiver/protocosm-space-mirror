package cfg

import (
	"time"

	"codeberg.org/reiver/go-env"
)

func HTTPHeaderReadTimeOut() time.Duration {
	return env.GetElse("HTTP_HEADER_READ_TIMEOUT", 15 * time.Second)
}

func HTTPIdleTimeOut() time.Duration {
	return env.GetElse("HTTP_IDLE_TIMEOUT", 120 * time.Second)
}

// HTTPInternalTcpAddress returns the TCP address of the internal listener — the one our own services call.
//
// It must be a private address. The default only listens on the loopback interface.
func HTTPInternalTcpAddress() string {
	return env.GetElse[string]("INTERNAL_ADDR", "127.0.0.1:8081")
}

// HTTPPublicTcpPort returns the TCP port of the public listener — the one space hosts call.
//
// Something in front of it terminates TLS on port 443.
func HTTPPublicTcpPort() uint16 {
	return env.GetElse[uint16]("PUBLIC_PORT", 8080)
}

func HTTPShutdownTimeOut() time.Duration {
	return env.GetElse("HTTP_SHUTDOWN_TIMEOUT", 30 * time.Second)
}
