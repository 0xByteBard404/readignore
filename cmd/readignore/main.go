// Package main 是 readignore CLI 入口（阶段5：委托给 internal/cli 调度命令）。
//
// main 只做一件事：调用 cli.Execute()。所有命令定义、参数解析、错误处理都在
// internal/cli 包内，便于复用与测试（main 包不易测试）。
package main

import "github.com/0xByteBard404/readignore/internal/cli"

func main() {
	cli.Execute()
}
