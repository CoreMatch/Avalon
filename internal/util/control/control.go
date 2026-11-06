// Package control 对应 net.rwhps.server.func.Control
package control

// EventNext 决定包/事件是否继续传递
type EventNext int

const (
	CONTINUE EventNext = iota
	STOPPED
)

// ControlFind 遍历控制
type ControlFind int

const (
	BREAK ControlFind = iota
	CONTINUE_LOOP
)

// ControlSync 同步控制
type ControlSync int

const (
	ASYNC ControlSync = iota
	SYNC
)

// ControlListening 监听控制
type ControlListening int

const (
	LISTENING ControlListening = iota
	STOPPED_LISTEN
)
