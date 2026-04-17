package tui

import (
	"fmt"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// PromptInput shows an interactive single-line text input and returns the
// user's response. It returns an empty string if the user presses Escape or
// Ctrl-C without submitting.
func PromptInput(prompt string) (string, error) {
	ti := textinput.New()
	ti.Prompt = prompt + " "
	ti.PromptStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
	ti.Focus()

	p := tea.NewProgram(promptModel{textInput: ti})
	m, err := p.Run()
	if err != nil {
		return "", fmt.Errorf("prompt: %w", err)
	}
	pm := m.(promptModel)
	if pm.cancelled {
		return "", nil
	}
	return pm.textInput.Value(), nil
}

type promptModel struct {
	textInput textinput.Model
	cancelled bool
	submitted bool
}

func (m promptModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m promptModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyEnter:
			m.submitted = true
			return m, tea.Quit
		case tea.KeyCtrlC, tea.KeyEsc:
			m.cancelled = true
			return m, tea.Quit
		}
	}

	var cmd tea.Cmd
	m.textInput, cmd = m.textInput.Update(msg)
	return m, cmd
}

func (m promptModel) View() string {
	if m.submitted || m.cancelled {
		return ""
	}
	return m.textInput.View()
}
