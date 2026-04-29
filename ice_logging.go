package renom

import (
	"fmt"
	"log"
	"net"
	"strings"

	"github.com/pion/logging"
)

type interfaceAwareLoggerFactory struct {
	base logging.LoggerFactory
}

func newInterfaceAwareLoggerFactory() logging.LoggerFactory {
	factory := logging.NewDefaultLoggerFactory()
	factory.DefaultLogLevel = logging.LogLevelInfo
	return &interfaceAwareLoggerFactory{base: factory}
}

func (f *interfaceAwareLoggerFactory) NewLogger(scope string) logging.LeveledLogger {
	return &interfaceAwareLogger{base: f.base.NewLogger(scope)}
}

type interfaceAwareLogger struct {
	base logging.LeveledLogger
}

func (l *interfaceAwareLogger) Trace(msg string) { l.base.Trace(annotateICELogMessage(msg)) }
func (l *interfaceAwareLogger) Tracef(format string, args ...any) {
	l.base.Trace(annotateICELogMessage(fmt.Sprintf(format, args...)))
}
func (l *interfaceAwareLogger) Debug(msg string) { l.base.Debug(annotateICELogMessage(msg)) }
func (l *interfaceAwareLogger) Debugf(format string, args ...any) {
	l.base.Debug(annotateICELogMessage(fmt.Sprintf(format, args...)))
}
func (l *interfaceAwareLogger) Info(msg string) { l.base.Info(annotateICELogMessage(msg)) }
func (l *interfaceAwareLogger) Infof(format string, args ...any) {
	l.base.Info(annotateICELogMessage(fmt.Sprintf(format, args...)))
}
func (l *interfaceAwareLogger) Warn(msg string) { l.base.Warn(annotateICELogMessage(msg)) }
func (l *interfaceAwareLogger) Warnf(format string, args ...any) {
	l.base.Warn(annotateICELogMessage(fmt.Sprintf(format, args...)))
}
func (l *interfaceAwareLogger) Error(msg string) { l.base.Error(annotateICELogMessage(msg)) }
func (l *interfaceAwareLogger) Errorf(format string, args ...any) {
	l.base.Error(annotateICELogMessage(fmt.Sprintf(format, args...)))
}

func annotateICELogMessage(msg string) string {
	ip, ok := readUDP4SourceIP(msg)
	if !ok {
		return msg
	}

	iface, err := interfaceNameForIP(ip)
	if err != nil {
		log.Printf("ICE log local address interface lookup failed ip=%s err=%v", ip, err)
		return fmt.Sprintf("%s local_ip=%s iface=unknown", msg, ip)
	}

	return fmt.Sprintf("%s local_ip=%s iface=%s", msg, ip, iface)
}

func readUDP4SourceIP(msg string) (string, bool) {
	const marker = "read udp4 "
	start := strings.Index(msg, marker)
	if start < 0 {
		return "", false
	}

	rest := msg[start+len(marker):]
	end := strings.Index(rest, ": ")
	if end < 0 {
		end = strings.IndexByte(rest, ' ')
	}
	if end < 0 {
		return "", false
	}

	addr := rest[:end]
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return "", false
	}
	if !isIPv4(net.ParseIP(host)) {
		return "", false
	}

	return host, true
}
