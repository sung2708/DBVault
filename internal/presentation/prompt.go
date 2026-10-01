package presentation

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/cancelreader"
	"github.com/sung2708/DBVault/internal/onboarding"
	"github.com/sung2708/DBVault/internal/security"
	"golang.org/x/term"
)

// CanPrompt is a shared policy: input and the prompt stream must both be TTYs.
// Machine output, quiet mode and explicit automation never open a prompt.
func CanPrompt(in io.Reader, out io.Writer, nonInteractive, machine, quiet bool) bool {
	f, ok := in.(interface{ Fd() uintptr })
	return ok && PromptAllowed(term.IsTerminal(int(f.Fd())), Detect(out).TTY, nonInteractive, machine, quiet)
}

func PromptAllowed(inputTTY, outputTTY, nonInteractive, machine, quiet bool) bool {
	return inputTTY && outputTTY && !nonInteractive && !machine && !quiet
}

type TerminalPrompter struct {
	In       io.Reader
	Out      io.Writer
	NoColor  bool
	Redactor *security.Redactor
}

func (p *TerminalPrompter) run(ctx context.Context, field huh.Field) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	keys := huh.NewDefaultKeyMap()
	keys.Quit.SetKeys("esc", "ctrl+c")
	keys.Quit.SetHelp("esc/ctrl+c", "cancel")
	keys.Select.Up.SetHelp("up/k", "up")
	keys.Select.Down.SetHelp("down/j", "down")
	form := huh.NewForm(huh.NewGroup(field)).WithInput(p.In).WithOutput(p.Out).WithTheme(p.theme()).WithKeyMap(keys)
	// No alternate-screen or mouse options: each question is an inline form.
	err := form.RunWithContext(ctx)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(err, huh.ErrUserAborted) {
		return context.Canceled
	}
	return err
}

func (p *TerminalPrompter) theme() *huh.Theme {
	theme := &huh.Theme{}
	theme.FieldSeparator = lipgloss.NewStyle().SetString("\n")
	theme.Focused.SelectSelector = lipgloss.NewStyle().SetString("> ")
	theme.Focused.ErrorIndicator = lipgloss.NewStyle().SetString("! ")
	theme.Focused.NextIndicator = lipgloss.NewStyle().SetString(">")
	theme.Focused.PrevIndicator = lipgloss.NewStyle().SetString("<")
	theme.Focused.FocusedButton = lipgloss.NewStyle().Underline(true).Padding(0, 1).MarginRight(1)
	theme.Focused.BlurredButton = lipgloss.NewStyle().Padding(0, 1)
	if !p.NoColor && os.Getenv("TERM") != "dumb" {
		_, disabled := os.LookupEnv("NO_COLOR")
		if !disabled {
			s := newStyles(true)
			theme.Focused.Title = s.accent
			theme.Focused.SelectSelector = s.accent.SetString("> ")
			theme.Focused.SelectedOption = s.accent
			theme.Focused.ErrorMessage = s.failure
		}
	}
	theme.Blurred = theme.Focused
	return theme
}
func (p *TerminalPrompter) Select(ctx context.Context, title string, choices []onboarding.Choice, value string) (string, error) {
	if os.Getenv("TERM") == "dumb" {
		p.Message(title)
		defaultIndex := 1
		for i, choice := range choices {
			p.Message(fmt.Sprintf("  %d. %s", i+1, choice.Label))
			if choice.Value == value {
				defaultIndex = i + 1
			}
		}
		for {
			answer, err := p.plainInput(ctx, "Choice number", strconv.Itoa(defaultIndex), false)
			if err != nil {
				return "", err
			}
			i, err := strconv.Atoi(answer)
			if err == nil && i >= 1 && i <= len(choices) {
				return choices[i-1].Value, nil
			}
			p.Message("Enter one of the listed numbers.")
		}
	}
	options := make([]huh.Option[string], len(choices))
	for i, c := range choices {
		options[i] = huh.NewOption(c.Label, c.Value)
	}
	err := p.run(ctx, huh.NewSelect[string]().Title(title).Options(options...).Value(&value))
	return value, err
}
func (p *TerminalPrompter) Input(ctx context.Context, title, value string) (string, error) {
	if os.Getenv("TERM") == "dumb" {
		return p.plainInput(ctx, title, value, false)
	}
	err := p.run(ctx, huh.NewInput().Title(title).Value(&value).Validate(func(s string) error {
		if strings.ContainsAny(s, "\x00\r\n\x1b") {
			return fmt.Errorf("control characters are not allowed")
		}
		return nil
	}))
	return value, err
}
func (p *TerminalPrompter) Secret(ctx context.Context, title string) (string, error) {
	if os.Getenv("TERM") == "dumb" {
		return p.plainInput(ctx, title, "", true)
	}
	var value string
	err := p.run(ctx, huh.NewInput().Title(title).Value(&value).EchoMode(huh.EchoModePassword).Validate(func(s string) error {
		if s == "" {
			return fmt.Errorf("a nonempty password is required for the connection test")
		}
		return nil
	}))
	return value, err
}
func (p *TerminalPrompter) Confirm(ctx context.Context, title string, value bool) (bool, error) {
	if os.Getenv("TERM") == "dumb" {
		fallback := "no"
		if value {
			fallback = "yes"
		}
		for {
			answer, err := p.plainInput(ctx, title+" (yes/no)", fallback, false)
			if err != nil {
				return false, err
			}
			switch strings.ToLower(answer) {
			case "y", "yes":
				return true, nil
			case "n", "no":
				return false, nil
			}
			p.Message("Enter yes or no.")
		}
	}
	err := p.run(ctx, huh.NewConfirm().Title(title).Value(&value))
	return value, err
}

// Minimal numbered fallback for TERM=dumb. It needs no cursor rendering or
// Unicode. The cancellable terminal reader restores terminal mode on every exit.
func (p *TerminalPrompter) plainInput(ctx context.Context, title, fallback string, secret bool) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	f, ok := p.In.(*os.File)
	if !ok {
		return "", fmt.Errorf("plain prompts require a terminal; use --non-interactive with flags")
	}
	state, err := term.MakeRaw(int(f.Fd()))
	if err != nil {
		return "", err
	}
	defer term.Restore(int(f.Fd()), state)
	r, err := cancelreader.NewReader(f)
	if err != nil {
		return "", err
	}
	defer r.Close()
	done, stopped := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(stopped)
		select {
		case <-ctx.Done():
			r.Cancel()
		case <-done:
		}
	}()
	defer func() { close(done); <-stopped }()
	if fallback != "" {
		title += " [" + fallback + "]"
	}
	fmt.Fprint(p.Out, title+": ")
	var input []byte
	defer func() {
		if secret {
			clear(input)
		}
	}()
	var b [1]byte
	for {
		_, err := r.Read(b[:])
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return "", context.Canceled
			}
			return "", err
		}
		switch b[0] {
		case 3, 27:
			fmt.Fprint(p.Out, "\r\n")
			return "", context.Canceled
		case '\r', '\n':
			fmt.Fprint(p.Out, "\r\n")
			if len(input) == 0 {
				return fallback, nil
			}
			return string(input), nil
		case 8, 127:
			if len(input) > 0 {
				_, size := utf8.DecodeLastRune(input)
				input = input[:len(input)-size]
				fmt.Fprint(p.Out, "\b \b")
			}
		default:
			if b[0] < 32 || len(input) >= 4096 {
				continue
			}
			input = append(input, b[0])
			if secret {
				fmt.Fprint(p.Out, "*")
			} else {
				fmt.Fprint(p.Out, string(b[:]))
			}
		}
	}
}
func (p *TerminalPrompter) Message(message string) {
	if p.Redactor != nil {
		message = p.Redactor.Text(message)
	}
	message = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, message)
	fmt.Fprintln(p.Out, message)
}
