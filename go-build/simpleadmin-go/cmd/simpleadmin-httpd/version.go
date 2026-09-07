package main

import "strings"

// appVersion 在构建时注入:
//
//	go build -ldflags "-s -w -X main.appVersion=3.00"
//
// Makefile 与 CI 使用版本号或 git 提交号注入;直接 go build 时使用源码默认值。
// 静态 HTML 中的 ?v=__SA_VERSION__ 占位符在服务时替换为该版本,
// 避免每次发版手工改十几处缓存参数。前端资源有改动时应递增该版本,
// 以便浏览器强制刷新缓存。
var appVersion = "3.00"

const appVersionPlaceholder = "__SA_VERSION__"

// themePlaceholder 出现在静态 HTML 的 data-bs-theme 属性上,
// 服务时替换为设备保存的默认主题(light/dark),
// 使无本地主题记录的浏览器首屏即按默认主题渲染,避免闪烁。
const themePlaceholder = "__SA_THEME__"

func renderStaticHTMLContent(content string) string {
	if !strings.Contains(content, appVersionPlaceholder) {
		return content
	}
	return strings.ReplaceAll(content, appVersionPlaceholder, appVersion)
}

// renderHTMLForServer 在版本号替换之外注入当前默认主题。
// 读取失败(文件不存在等)时 readThemeConfig 返回 defaultTheme,渲染不受影响。
func (s *simpleAdminServer) renderHTMLForServer(content string) string {
	theme, _ := readThemeConfig(s.themeConfigPath())
	if strings.Contains(content, themePlaceholder) {
		content = strings.ReplaceAll(content, themePlaceholder, theme)
	}
	return renderStaticHTMLContent(content)
}
