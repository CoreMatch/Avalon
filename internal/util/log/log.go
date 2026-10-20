// Package log 复刻 net.rwhps.server.util.log.Log / LogCore 的行为.
//
// 为避免 Go 的循环导入, 本包通过包级变量接收外部依赖:
//   - LogPath        : 日志文件目录 (默认 /data/log)
//   - ConsoleHook    : ServerConsolePrintEvent 的触发钩子
package log

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Level 对应 LogCore.Logg
type Level struct {
	Level int
	Tag   string
	Name  string
}

var (
	LevelOff     = Level{99, "", "OFF"}
	LevelConsole = Level{10, "", "CONSOLE"}
	LevelFatal   = Level{7, cBackRed + "[FATAL]&fr", "FATAL"}
	LevelError   = Level{6, "&r[ERROR]&fr", "ERROR"}
	LevelWarn    = Level{5, "&y[WARN]&fr", "WARN"}
	LevelInfo    = Level{4, "&g[INFO]&fr", "INFO"}
	LevelDebug   = Level{3, "\u001B[38;2;128;128;128m[DEBUG]&fr", "DEBUG"}
	LevelTrack   = Level{2, "\u001B[38;2;128;128;128m[TRACK]&fr", "TRACK"}
	LevelAll     = Level{1, "[All]", "ALL"}
)

var allLevels = []Level{LevelOff, LevelConsole, LevelFatal, LevelError, LevelWarn, LevelInfo, LevelDebug, LevelTrack, LevelAll}

// FromLevel 对应 Logg.from(type)
func FromLevel(t string) Level {
	for _, l := range allLevels {
		if l.Name == t || strings.EqualFold(l.Name, t) {
			return l
		}
	}
	Skipping(fmt.Sprintf("Log Level Set Error , In: %s", t))
	return LevelAll
}

var (
	mu       sync.Mutex
	logGrade = LevelWarn
	logFile  *os.File
	logDev   = "/data/log"
	logPath  string
)

// LogPath 设置日志目录
func LogPath(dir string) {
	mu.Lock()
	defer mu.Unlock()
	logDev = dir
	logPath = filepath.Join(dir, "Log.txt")
	initLogFileLocked()
}

func initLogFileLocked() {
	if logPath == "" {
		logPath = filepath.Join(logDev, "Log.txt")
	}
	_ = os.MkdirAll(filepath.Dir(logPath), 0o755)
	truncate := true
	if fi, err := os.Stat(logPath); err == nil && fi.Size() > 512*1024 {
		truncate = false
	}
	flags := os.O_CREATE | os.O_WRONLY | os.O_APPEND
	if truncate {
		flags = os.O_CREATE | os.O_WRONLY | os.O_TRUNC
	}
	f, err := os.OpenFile(logPath, flags, 0o644)
	if err == nil {
		logFile = f
	}
}

// Set 设置日志等级
func Set(level string) {
	mu.Lock()
	defer mu.Unlock()
	logGrade = FromLevel(level)
}

// SaveLog 刷盘
func SaveLog() {
	mu.Lock()
	defer mu.Unlock()
	if logFile != nil {
		_ = logFile.Sync()
	}
}

// ConsoleHook 由事件系统注入, 用于触发 ServerConsolePrintEvent
var ConsoleHook func(string)

func getMilliFormat() string {
	now := time.Now()
	return now.Format("2006-01-02 15:04:05.") + fmt.Sprintf("%03d", now.Nanosecond()/1e6)
}

func logs(l Level, tag any, eIn any) {
	errorFlag := false
	if _, ok := tag.(error); ok {
		errorFlag = true
	}
	if _, ok := eIn.(error); ok {
		errorFlag = true
	}

	mu.Lock()
	grade := logGrade
	mu.Unlock()

	if grade.Level > l.Level && !errorFlag {
		return
	}

	var body string
	switch v := eIn.(type) {
	case nil:
		body = ""
	case error:
		body = resolveTrace(v)
	default:
		body = fmt.Sprint(v)
	}

	var b strings.Builder
	b.WriteString("[")
	b.WriteString(getMilliFormat())
	b.WriteString("]")
	b.WriteString(l.Tag)
	b.WriteString("&b[$ThreadName]&fr")

	if _, isErr := tag.(error); !isErr && tag != "" && tag != nil {
		b.WriteString(" ")
		b.WriteString(fmt.Sprint(tag))
	}

	lines := strings.Split(body, "\n")
	if len(lines) > 0 && lines[0] != "" {
		b.WriteString(": ")
	}
	if len(lines) > 1 {
		b.WriteString("\n")
		flag := false
		for _, line := range lines {
			if strings.TrimSpace(line) != "" {
				if flag {
					b.WriteString("\n")
				}
				b.WriteString(line)
				flag = true
			}
		}
	} else if len(lines) == 1 {
		b.WriteString(lines[0])
	}
	b.WriteString("&fr")

	text := FormatColors(parseStr(b.String()), false)

	// 输出到 stdout (Go 中 stdout/stderr 无需替换)
	fmt.Fprintln(os.Stdout, text)

	if ConsoleHook != nil {
		ConsoleHook(text)
	}

	if errorFlag {
		mu.Lock()
		if logFile != nil {
			_, _ = logFile.WriteString(StripColors(text) + "\n")
		}
		mu.Unlock()
	}
}

func parseStr(text string) string {
	return strings.ReplaceAll(text, "$ThreadName", threadName())
}

var mainGID int

func init() {
	mainGID = goroutineID()
}

func isMainGoroutine() bool {
	return goroutineID() == mainGID
}

func goroutineID() int {
	var buf [64]byte
	n := runtime.Stack(buf[:], false)
	fields := strings.Fields(string(buf[:n]))
	if len(fields) >= 2 {
		if id, err := strconv.Atoi(fields[1]); err == nil {
			return id
		}
	}
	return 0
}

func threadName() string {
	// Go 没有线程名概念; 主 goroutine 标记为 Main, 其余用 goroutine id 近似
	if isMainGoroutine() {
		return "Main"
	}
	return "Go-" + strconv.Itoa(goroutineID())
}

func Skipping(v any)              { logs(LevelOff, v, nil) }
func Skippingf(v any, args ...any) { logs(LevelOff, v, fmt.Sprint(args...)) }
func Fatal(v any)                 { logs(LevelFatal, v, nil) }
func Fatalf(v any, args ...any)   { logs(LevelFatal, v, fmt.Sprint(args...)) }
func Error(v any)                 { logs(LevelError, v, nil) }
func Errorf(v any, args ...any)   { logs(LevelError, v, fmt.Sprint(args...)) }
func Warn(v any)                  { logs(LevelWarn, v, nil) }
func Warnf(v any, args ...any)    { logs(LevelWarn, v, fmt.Sprint(args...)) }
func Info(v any)                  { logs(LevelInfo, v, nil) }
func Infof(v any, args ...any)    { logs(LevelInfo, v, fmt.Sprint(args...)) }
func Debug(v any)                 { logs(LevelDebug, v, nil) }
func Debugf(v any, args ...any)   { logs(LevelDebug, v, fmt.Sprint(args...)) }
func Track(v any)                 { logs(LevelTrack, v, nil) }
func Trackf(v any, args ...any)   { logs(LevelTrack, v, fmt.Sprint(args...)) }
func All(v any)                   { logs(LevelAll, v, nil) }

// Clog 对应 Log.clog (CONSOLE 级别)
func Clog(v any, args ...any) { logs(LevelConsole, "", fmt.Sprint(v)) }

// LogError 记录一个 error (带堆栈)
func LogError(tag any, e error) { logs(LevelError, tag, e) }

func resolveTrace(e error) string {
	buf := make([]byte, 8192)
	n := runtime.Stack(buf, false)
	return e.Error() + "\n" + string(buf[:n])
}
