package presentation

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/sung2708/DBVault/internal/security"
)

func TestPromptPolicy(t *testing.T) {
	for _, tc := range []struct{ in, out, noninteractive, json, quiet, want bool }{
		{true, true, false, false, false, true}, {false, true, false, false, false, false},
		{true, false, false, false, false, false}, {true, true, true, false, false, false},
		{true, true, false, true, false, false}, {true, true, false, false, true, false},
	} {
		if got := PromptAllowed(tc.in, tc.out, tc.noninteractive, tc.json, tc.quiet); got != tc.want {
			t.Fatal(tc, got)
		}
	}
	if CanPrompt(strings.NewReader(""), &bytes.Buffer{}, false, false, false) {
		t.Fatal("non-TTY permitted")
	}
}
func TestPromptColorsMessagesAndCancellation(t *testing.T) {
	for _, noColor := range []bool{false, true} {
		t.Run(fmt.Sprintf("noColor=%t", noColor), func(t *testing.T) {
			t.Setenv("NO_COLOR", "") // presence, even empty, disables color
			var out bytes.Buffer
			p := &TerminalPrompter{Out: &out, NoColor: noColor, Redactor: security.New("secret-value")}
			theme := p.theme()
			if strings.Contains(theme.Focused.Title.Render("title")+theme.Focused.SelectSelector.Render(">"), "\x1b[") {
				t.Fatal("NO_COLOR ignored")
			}
			p.Message("host\x1b[2J secret-value postgres://u:p@host")
			if strings.Contains(out.String(), "secret-value") || strings.Contains(out.String(), "u:p@") || strings.Contains(out.String(), "\x1b") {
				t.Fatal("unsafe message", out.String())
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			_, err := p.Input(ctx, "Host", "")
			if !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
		})
	}
}
