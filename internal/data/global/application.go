// Package global 对应 net.rwhps.server.data.global
package global

import (
	"crypto/rand"
	"fmt"
	"os"
	"strings"
	"sync"

	"rwhps/internal/net/manage"
	"rwhps/internal/util/algorithms"
	"rwhps/internal/util/file"
	"rwhps/internal/util/file/plugindata"
	"rwhps/internal/util/log"
	"rwhps/internal/util/mathutil"
)

// Core 对应 Data.core (Application 单例)
var Core *Application

// CloseServerHook 对应 NetServer.closeServer(), 由 app 层注入
var CloseServerHook = func() {}

// Application 对应 net.rwhps.server.core.Application
type Application struct {
	// Settings 对应 settings (data/Settings.bin)
	Settings *plugindata.Settings

	// ServerConnectUUID 对应 serverConnectUuid (持久化)
	ServerConnectUUID string
	// ServerHessUUID 对应 serverHessUuid (持久化)
	ServerHessUUID string
	// ServerToken 对应 serverToken (每次启动重新生成)
	ServerToken string

	// Admin 对应 admin
	Admin *Administration
	// UpServerList 对应 upServerList
	UpServerList bool

	// HTTP 对应 http
	HTTP *manage.HttpRequestManage
	// RWHTTP 对应 rwHttp
	RWHTTP *manage.HttpRequestManage

	mu        sync.Mutex
	saveHooks []func()
}

// NewApplication 对应 Application() 的构造过程
func NewApplication() *Application {
	settings, err := plugindata.Open(file.GetFolder(ServerDataPath).ToFile("Settings.bin"))
	if err != nil {
		// 原版解码失败会抛异常; 这里降级为只读, 保证不覆盖既有身份文件
		log.Errorf("[Settings]", "{0} (store is now read-only)", err)
	}

	a := &Application{
		Settings: settings,
		Admin:    NewAdministration(),
		HTTP:     manage.NewHttpRequestManage(UserAgent),
		RWHTTP:   manage.NewHttpRequestManage("RW-HPS " + ServerCoreVersion),
	}

	a.ServerConnectUUID = settings.GetString("serverConnectUuid", NewUUID)
	a.ServerHessUUID = settings.GetString("serverHessUuid", func() string {
		sum := algorithms.SHA256([]byte(a.ServerConnectUUID + NewUUID()))
		return strings.ToUpper(algorithms.BigIntegerHex(sum))
	})
	a.ServerToken = mathutil.GetRandomString(40)
	return a
}

// AddSaveHook 对应 Threads.addSavePool: 退出时依次执行
func (a *Application) AddSaveHook(fn func()) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.saveHooks = append(a.saveHooks, fn)
}

// Save 对应 Application.save(): 先跑保存队列, 再落盘 Settings
func (a *Application) Save() {
	a.mu.Lock()
	hooks := make([]func(), len(a.saveHooks))
	copy(hooks, a.saveHooks)
	a.mu.Unlock()

	for _, fn := range hooks {
		fn()
	}
	if a.Settings != nil {
		if err := a.Settings.Save(); err != nil {
			log.Errorf("[Settings]", "save failed: {0}", err)
		}
	}
}

// Exit 对应 Application.exit(): 关服务 -> 保存 -> 执行回调 -> 退出
func (a *Application) Exit(run func()) {
	defer func() {
		if run != nil {
			run()
		}
	}()
	CloseServerHook()
	a.Save()
	os.Exit(0)
}

// MandatoryExit 对应 mandatoryExit()
func (a *Application) MandatoryExit() {
	os.Exit(0)
}

// NewUUID 对应 UUID.randomUUID().toString(): 小写 v4, 带连字符
func NewUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand 失败极罕见; 退化为时间无关的固定形态, 保证长度与格式正确
		return "00000000-0000-4000-8000-000000000000"
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
