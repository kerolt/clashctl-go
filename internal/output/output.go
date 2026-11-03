// Package output 负责格式化 clashctl 终端消息。
package output

import (
	"fmt"
	"io"
)

// Printer writes colored status messages to command output streams.
type Printer struct {
	Out io.Writer
	Err io.Writer
}

// New constructs a Printer with separate normal and error streams.
func New(out, errOut io.Writer) *Printer {
	return &Printer{Out: out, Err: errOut}
}

// Color writes message using an RGB ANSI foreground color.
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

// OK writes a successful status message.
func (p *Printer) OK(message string) {
	p.Color(p.Out, "#c8d6e5", "😼 "+message)
}

// OKWithEmoji writes a successful status message with a custom emoji.
func (p *Printer) OKWithEmoji(emoji, message string) {
	p.Color(p.Out, "#c8d6e5", emoji+" "+message)
}

// Fail writes a recoverable failure message.
func (p *Printer) Fail(message string) {
	p.Color(p.Err, "#fd79a8", "😾 "+message)
}

// FailWithEmoji writes a recoverable failure message with a custom emoji.
func (p *Printer) FailWithEmoji(emoji, message string) {
	p.Color(p.Err, "#fd79a8", emoji+" "+message)
}

// Error writes an error message to the error stream.
func (p *Printer) Error(message string) {
	p.Color(p.Err, "#f92f60", "📢 "+message)
}

// parseHex parses a two-digit hexadecimal color component.
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
