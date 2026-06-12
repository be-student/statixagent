package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Question indices into wizard.answers.
const (
	qToken = iota
	qChatID
	qCPU
	qDisk
	qTemp
	qBattery
	qServices
	qProcesses
	qPorts
	numQuestions
)

// inputStep describes one free-text question.
type inputStep struct {
	q           int
	title       string
	guidance    string
	placeholder string
	secret      bool
}

var inputSteps = []inputStep{
	{qToken, "Telegram bot token",
		"Open Telegram, talk to @BotFather → /newbot → copy the token.", "123456789:AAF...", true},
	{qChatID, "Telegram chat ID",
		"Send any message to your new bot, then visit\nhttps://api.telegram.org/bot<TOKEN>/getUpdates and copy message.chat.id.\n(Or ask @userinfobot for your ID.)", "123456789", false},
	{qCPU, "CPU alert threshold (%)", "Alert when total CPU stays above this.", "90", false},
	{qDisk, "Disk alert threshold (%)", "Alert when any mount fills past this.", "85", false},
	{qTemp, "Temperature alert threshold (°C)", "Alert when the hottest sensor passes this.", "85", false},
	{qBattery, "Battery alert threshold (%)", "Alert when discharging below this.", "15", false},
	{qServices, "systemd services to watch", "Comma-separated, e.g.: nginx, postgresql ('.service' added automatically). Empty = none.", "nginx, docker", false},
	{qProcesses, "Processes to watch", "Comma-separated executable names, e.g.: node, sshd. Empty = none.", "", false},
	{qPorts, "Local TCP ports to check", "Comma-separated, e.g.: 22, 443. Empty = none.", "", false},
}

// toggle is one on/off choice.
type toggle struct {
	label string
	on    bool
}

// phases: text questions → toggles screen → confirm screen.
const (
	phaseInputs = iota
	phaseToggles
	phaseConfirm
)

type wizard struct {
	phase    int
	step     int // index into inputSteps during phaseInputs
	cursor   int // toggle cursor during phaseToggles
	input    textinput.Model
	answers  [numQuestions]string
	toggles  []toggle
	errMsg   string
	aborted  bool
	finished bool
}

var (
	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))
	guideStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	errStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	selectedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("212"))
)

func newWizard() wizard {
	w := wizard{
		toggles: []toggle{
			{"Core system (CPU/mem/disk/net)", true},
			{"Thermal & hardware", true},
			{"Battery & power", true},
			{"Docker containers", true},
			{"SSH & security", true},
			{"Automatic self-update (opt-in)", false},
		},
	}
	w.input = newInput(inputSteps[0])
	return w
}

func newInput(s inputStep) textinput.Model {
	in := textinput.New()
	in.Placeholder = s.placeholder
	if s.secret {
		in.EchoMode = textinput.EchoPassword
	}
	in.Focus()
	return in
}

func (w wizard) Init() tea.Cmd { return textinput.Blink }

func (w wizard) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, isKey := msg.(tea.KeyMsg)
	if isKey {
		switch key.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			w.aborted = true
			return w, tea.Quit
		}
	}

	switch w.phase {
	case phaseInputs:
		if isKey && key.Type == tea.KeyEnter {
			val := strings.TrimSpace(w.input.Value())
			st := inputSteps[w.step]
			if val == "" && st.placeholder != "" && (st.q == qCPU || st.q == qDisk || st.q == qTemp || st.q == qBattery) {
				val = st.placeholder // accept default thresholds on empty enter
			}
			if (st.q == qToken || st.q == qChatID) && val == "" {
				w.errMsg = "this field is required"
				return w, nil
			}
			w.errMsg = ""
			w.answers[st.q] = val
			w.step++
			if w.step >= len(inputSteps) {
				w.phase = phaseToggles
				return w, nil
			}
			w.input = newInput(inputSteps[w.step])
			return w, textinput.Blink
		}
		var cmd tea.Cmd
		w.input, cmd = w.input.Update(msg)
		return w, cmd

	case phaseToggles:
		if isKey {
			switch key.String() {
			case "up", "k":
				if w.cursor > 0 {
					w.cursor--
				}
			case "down", "j":
				if w.cursor < len(w.toggles)-1 {
					w.cursor++
				}
			case " ":
				w.toggles[w.cursor].on = !w.toggles[w.cursor].on
			case "enter":
				w.phase = phaseConfirm
			}
		}
		return w, nil

	case phaseConfirm:
		if isKey {
			switch key.String() {
			case "y", "Y", "enter":
				w.finished = true
				return w, tea.Quit
			case "n", "N":
				w.aborted = true
				return w, tea.Quit
			}
		}
		return w, nil
	}
	return w, nil
}

func (w wizard) View() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("StatixAgent installer") + "\n\n")

	switch w.phase {
	case phaseInputs:
		st := inputSteps[w.step]
		fmt.Fprintf(&b, "Step %d/%d — %s\n", w.step+1, len(inputSteps)+2, titleStyle.Render(st.title))
		b.WriteString(guideStyle.Render(st.guidance) + "\n\n")
		b.WriteString(w.input.View() + "\n")
		if w.errMsg != "" {
			b.WriteString(errStyle.Render("✗ "+w.errMsg) + "\n")
		}
		b.WriteString(guideStyle.Render("\nenter: next · esc: cancel"))

	case phaseToggles:
		fmt.Fprintf(&b, "Step %d/%d — %s\n\n", len(inputSteps)+1, len(inputSteps)+2,
			titleStyle.Render("What should this agent monitor?"))
		for i, t := range w.toggles {
			mark := "[ ]"
			if t.on {
				mark = "[x]"
			}
			line := fmt.Sprintf("%s %s", mark, t.label)
			if i == w.cursor {
				line = selectedStyle.Render("› " + line)
			} else {
				line = "  " + line
			}
			b.WriteString(line + "\n")
		}
		b.WriteString(guideStyle.Render("\nspace: toggle · ↑/↓: move · enter: continue · esc: cancel"))

	case phaseConfirm:
		fmt.Fprintf(&b, "Step %d/%d — %s\n\n", len(inputSteps)+2, len(inputSteps)+2,
			titleStyle.Render("Ready to install"))
		b.WriteString("Will write /etc/statix-agent/config.toml (0600),\n")
		b.WriteString("install /usr/local/bin/statix-agent, and enable the systemd service.\n\n")
		fmt.Fprintf(&b, "  chat ID: %s\n", w.answers[qChatID])
		on := []string{}
		for _, t := range w.toggles[:5] {
			if t.on {
				on = append(on, strings.SplitN(t.label, " ", 2)[0])
			}
		}
		fmt.Fprintf(&b, "  monitors: %s\n", strings.Join(on, ", "))
		fmt.Fprintf(&b, "  auto-update: %v\n\n", w.toggles[5].on)
		b.WriteString(titleStyle.Render("Install now? (y/n)"))
	}
	return b.String() + "\n"
}
