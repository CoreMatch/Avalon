// Hello 插件: 最小可跑的 Avalon 插件.
//
// 运行:
//
//	go run . -addr 127.0.0.1:9800 -secret <与 Avalon 共享的 hex 密钥>
//
// 然后在 Avalon 的 /data/Plugins.json 里登记 (见 README.md):
// Avalon 启动/重启后会来拉 manifest 完成握手, 之后按订阅推送事件.
// 插件不需要主动连接 Avalon —— 方向是 Avalon 调用插件.
package main

import (
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"os"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:9800", "监听地址")
	secretHex := flag.String("secret", "", "与 Avalon 共享的密钥 (hex, 与 Plugins.json 的 secrets 一致)")
	deny := flag.Bool("deny", false, "演示用: 否决全部 room.create 建房准入")
	flag.Parse()

	if *secretHex == "" {
		log.Fatal("必须提供 -secret (hex)")
	}
	secret, err := DecodeHexSecret(*secretHex)
	if err != nil {
		log.Fatalf("secret: %v", err)
	}

	p := NewPlugin("hello-plugin", "0.1.0", secret)

	// --- room.create (否决档): 建房前同步准入 ---
	// 应答必须是 Verdict; allowed=false 时 reason 必填 (无理由会被当畸形
	// 应答 fail-open, 否决不生效).
	p.Handle("room.create", true, func(body []byte) (int, []byte) {
		log.Printf("[room.create] %s", body)
		if *deny {
			return http.StatusOK, VerdictJSON(Verdict{Allowed: false, Reason: "hello 插件演示否决: 维护中"})
		}
		return http.StatusOK, VerdictJSON(Verdict{Allowed: true})
	})

	// --- game.over (投递档): 对局结束结算, 至少一次, 按 eventId 去重 ---
	p.Handle("game.over", false, func(body []byte) (int, []byte) {
		var ev struct {
			EventID string `json:"eventId"`
			Result  *struct {
				MapName  string   `json:"mapName"`
				GameTime int      `json:"gameTime"`
				Winners  []string `json:"winPlayerList"`
			} `json:"result"`
		}
		if err := json.Unmarshal(body, &ev); err != nil {
			// 畸形载荷: 返回 400 让 Avalon 计入投递失败 (会重试).
			// 如果你的处理逻辑对坏数据只想跳过, 返回 200 即可.
			return http.StatusBadRequest, nil
		}
		log.Printf("[game.over] id=%s result=%+v (记得按 eventId 去重 ≥24h)", ev.EventID, ev.Result)
		return http.StatusOK, []byte(`{}`)
	})

	// --- player.kicked (观察档): 踢出审计, 尽力投递 ---
	p.Handle("player.kicked", false, func(body []byte) (int, []byte) {
		log.Printf("[player.kicked] %s", body)
		return http.StatusOK, []byte(`{}`)
	})

	log.Printf("hello plugin listening on %s (id=%s)", *addr, p.ID)
	if err := http.ListenAndServe(*addr, p); err != nil {
		log.Fatal(err)
		os.Exit(1)
	}
}
