package ui

import (
	"fmt"
	"os"

	"github.com/charmbracelet/lipgloss"
)

var (
	brand    = lipgloss.NewStyle().Foreground(lipgloss.Color("99")).Bold(true)
	success  = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	warn     = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	errStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	dim      = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	key      = lipgloss.NewStyle().Foreground(lipgloss.Color("39")).Bold(true)
	val      = lipgloss.NewStyle().Foreground(lipgloss.Color("255"))
)

func Brand(s string) string { return brand.Render(s) }
func Dim(s string) string   { return dim.Render(s) }
func Key(s string) string   { return key.Render(s) }
func Val(s string) string   { return val.Render(s) }

func Success(format string, a ...any) {
	fmt.Fprintln(os.Stderr, success.Render("✓ "+fmt.Sprintf(format, a...)))
}

func Warn(format string, a ...any) {
	fmt.Fprintln(os.Stderr, warn.Render("! "+fmt.Sprintf(format, a...)))
}

func Error(format string, a ...any) {
	fmt.Fprintln(os.Stderr, errStyle.Render("✗ "+fmt.Sprintf(format, a...)))
}

func Info(format string, a ...any) {
	fmt.Fprintln(os.Stderr, fmt.Sprintf(format, a...))
}

func KV(k, v string) {
	fmt.Fprintf(os.Stderr, "  %s  %s\n", key.Render(k), val.Render(v))
}
