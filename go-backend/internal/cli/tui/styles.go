package tui

import "github.com/charmbracelet/lipgloss"

var (
	// Color palette - dark terminal theme
	accent    = lipgloss.Color("#6C8CFF")   // soft blue
	accentDim = lipgloss.Color("#4A5FAF")   // dimmer blue
	green     = lipgloss.Color("#73D18D")   // soft green
	yellow    = lipgloss.Color("#E5C06E")   // warm amber
	orange    = lipgloss.Color("#E0A060")   // orange
	red       = lipgloss.Color("#E0556A")   // soft red
	cyan      = lipgloss.Color("#5EC4E0")   // cyan
	magenta   = lipgloss.Color("#C58CE3")   // purple
	gray      = lipgloss.Color("#6E6E7A")   // medium gray
	grayDark  = lipgloss.Color("#3A3A44")   // dark gray
	grayLight = lipgloss.Color("#9E9EA8")   // light gray
	bgDark    = lipgloss.Color("#1A1A22")   // near-black bg
	bgMid     = lipgloss.Color("#22222C")   // mid-dark bg
	textWhite = lipgloss.Color("#E3E3EB")   // off-white text
	textDim   = lipgloss.Color("#888897")   // dim text

	// Base text style
	textStyle = lipgloss.NewStyle().Foreground(textWhite)

	// Header
	headerStyle = lipgloss.NewStyle().
			Background(bgMid).
			Padding(0, 1)

	orchLogo = lipgloss.NewStyle().
			Foreground(accent).
			Bold(true).
			Render("orch")

	// User message
	userMsgStyle = lipgloss.NewStyle().
			Foreground(accent).
			Bold(true)

	userPrefix = lipgloss.NewStyle().
			Foreground(accentDim).
			Render("❯")

	// Assistant message style
	assistantStyle = lipgloss.NewStyle().
			Foreground(textWhite)

	// Thinking block
	thinkBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder(), false, false, false, true).
			BorderForeground(grayDark).
			PaddingLeft(2).
			Width(100)

	thinkHeaderStyle = lipgloss.NewStyle().
				Foreground(cyan).
				Faint(true).
				Italic(true)

	thinkBodyStyle = lipgloss.NewStyle().
			Foreground(textDim).
			Italic(true)

	// Tool calls
	toolBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder(), false, false, false, true).
			BorderForeground(grayDark).
			PaddingLeft(2).
			Width(100)

	toolNameStyle = lipgloss.NewStyle().
			Foreground(yellow).
			Bold(true)

	toolArgsStyle = lipgloss.NewStyle().
			Foreground(textDim)

	toolRunningIcon = lipgloss.NewStyle().
			Foreground(yellow).
			SetString("●")

	toolSuccessIcon = lipgloss.NewStyle().
			Foreground(green).
			SetString("✓")

	toolErrorIcon = lipgloss.NewStyle().
			Foreground(red).
			SetString("✗")

	toolResultStyle = lipgloss.NewStyle().
			Foreground(textDim).
			PaddingLeft(2)

	toolResultBorder = lipgloss.NewStyle().
			Foreground(grayDark).
			SetString("│")

	// Code block
	codeBlockStyle = lipgloss.NewStyle().
			Background(bgMid).
			Padding(1, 2).
			Width(100)

	// Error
	errorStyle = lipgloss.NewStyle().
			Foreground(red).
			Bold(true)

	// Approval
	approvalBoxStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(orange).
				Padding(0, 1).
				Width(100)

	approvalTitleStyle = lipgloss.NewStyle().
				Foreground(orange).
				Bold(true)

	approvalKeyStyle = lipgloss.NewStyle().
			Foreground(green).
			Bold(true)

	approvalRiskLow = lipgloss.NewStyle().
			Foreground(green).
			Render("LOW")

	approvalRiskHigh = lipgloss.NewStyle().
			Foreground(red).
			Render("HIGH")

	// Artifact
	artifactStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(accent).
			Padding(0, 1)

	// Status bar
	statusBarStyle = lipgloss.NewStyle().
			Background(bgMid).
			Foreground(textDim).
			Padding(0, 1).
			Width(100)

	statusKeyStyle = lipgloss.NewStyle().
			Background(grayDark).
			Foreground(textDim).
			Padding(0, 1)

	statusInfoStyle = lipgloss.NewStyle().
			Foreground(textDim)

	// Input area
	inputBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(grayDark)

	inputPromptStyle = lipgloss.NewStyle().
			Foreground(accent).
			Bold(true).
			SetString(">")

	inputStyle = lipgloss.NewStyle().
			Foreground(textWhite)

	// Help overlay
	helpBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(accentDim).
			Padding(1, 2).
			Background(bgMid)

	helpTitleStyle = lipgloss.NewStyle().
			Foreground(accent).
			Bold(true)

	helpKeyStyle = lipgloss.NewStyle().
			Foreground(green)

	helpDescStyle = lipgloss.NewStyle().
			Foreground(textDim)

	// Section labels
	sectionStyle = lipgloss.NewStyle().
			Foreground(accentDim).
			Faint(true).
			Padding(0, 0, 0, 1)
)
