# assets

原版资源文件的逐字节拷贝, 通过 `go:embed` 打进二进制.

| 目录/文件 | 来源 |
|---|---|
| `bundles/HPS_{en_US,zh_CN,zh_HK,ru_RU}.properties` | `Server-Core/src/main/resources/bundles/` |
| `eula/Main-{China,English}.md` | `Server-Core/src/main/resources/eula/` |
| `URL.ini` | `Server-Core/src/main/resources/URL.ini` |

原版更新这些文件后需要同步拷贝过来; `resource_test.go` 会校验文件存在且可解析.
