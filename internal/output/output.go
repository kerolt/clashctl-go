// Package output 负责格式化 clashctl 终端消息。
package output

import (
	"fmt"
	"io"
)

// Printer 向命令输出流写入带颜色的状态信息。
type Printer struct {
	Out io.Writer
	Err io.Writer
}

// New 构造分离正常输出与错误输出的 Printer。
func New(out, errOut io.Writer) *Printer {
	return &Printer{Out: out, Err: errOut}
}

// Color 用 RGB 前景色写入一条消息。
func (p *Printer) Color(writer io.Writer, color, message string) {
	if len(color) != 7 || color[0] != '#' {
		fmt.Fprintln(writer, message)
		return
	}
	red := parseHex(color[1:3])
	green := parseHex(color[3:5])
	blue := parseHex(color[5:7])
	fmt.Fprintf(writer, "\033[38;2;%d;%d;%dm%s\033[0m\n", red, green, blue, message)
}

// OK 写入成功状态消息。
func (p *Printer) OK(message string) {
	p.Color(p.Out, "#c8d6e5", "😼 "+message)
}

// OKWithEmoji 用自定义表情写入成功状态消息。
func (p *Printer) OKWithEmoji(emoji, message string) {
	p.Color(p.Out, "#c8d6e5", emoji+" "+message)
}

// Fail 写入可恢复的失败消息。
func (p *Printer) Fail(message string) {
	p.Color(p.Err, "#fd79a8", "😾 "+message)
}

// FailWithEmoji 用自定义表情写入可恢复的失败消息。
func (p *Printer) FailWithEmoji(emoji, message string) {
	p.Color(p.Err, "#fd79a8", emoji+" "+message)
}

// Error 向错误输出写入错误消息。
func (p *Printer) Error(message string) {
	p.Color(p.Err, "#f92f60", "📢 "+message)
}

// parseHex 解析两位十六进制颜色分量。
func parseHex(value string) int {
	result := 0
	for _, char := range value {
		result *= 16
		switch {
		case char >= '0' && char <= '9':
			result += int(char - '0')
		case char >= 'a' && char <= 'f':
			result += int(char-'a') + 10
		case char >= 'A' && char <= 'F':
			result += int(char-'A') + 10
		}
	}
	return result
}
