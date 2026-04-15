package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/janekbaraniewski/openusage/internal/core"
)

type DetailTab int

const (
	TabAll  DetailTab = 0 // show everything
	TabDyn1 DetailTab = 1 // first dynamic group
)

// detailSection represents a renderable section in the detail view.
type detailSection struct {
	id           string
	title        string
	icon         string
	color        lipgloss.Color
	lines        []string
	hasOwnHeader bool // true when lines already contain a styled heading (composition sections)
}

func DetailTabs(snap core.UsageSnapshot) []string {
	// Single scrollable dashboard — no tabs needed.
	// All sections are shown in a well-organized card layout.
	return []string{"All"}
}

func RenderDetailContent(snap core.UsageSnapshot, w int, warnThresh, critThresh float64, activeTab int, timeWindow core.TimeWindow) string {
	var sb strings.Builder
	widget := dashboardWidget(snap.ProviderID)

	// ── Compact top bar ──
	renderDetailCompactHeader(&sb, snap, w)

	if len(snap.Metrics) == 0 && len(snap.ModelUsage) == 0 {
		if snap.Message != "" {
			sb.WriteString("\n")
			sb.WriteString(dimStyle.Render("  " + snap.Message))
			sb.WriteString("\n")
		}
		return sb.String()
	}

	// Build and render all sections as bordered cards.
	sections := buildDetailSections(snap, widget, w, warnThresh, critThresh, timeWindow)
	for _, sec := range sections {
		renderDetailCard(&sb, sec, w)
	}

	return sb.String()
}

// ── Compact Header ─────────────────────────────────────────────────────────
// Replaces the old bordered card header. Shows essential info in 2 lines.

func renderDetailCompactHeader(sb *strings.Builder, snap core.UsageSnapshot, w int) {
	di := computeDisplayInfo(snap, dashboardWidget(snap.ProviderID))

	// Line 1: status icon + name (left) ... provider + meta + status badge (right)
	statusIcon := lipgloss.NewStyle().Foreground(StatusColor(snap.Status)).Render(StatusIcon(snap.Status))
	name := lipgloss.NewStyle().Bold(true).Foreground(colorText).Render(snap.AccountID)

	var rightParts []string
	if di.tagEmoji != "" && di.tagLabel != "" {
		rightParts = append(rightParts, lipgloss.NewStyle().Foreground(tagColor(di.tagLabel)).Render(di.tagEmoji+" "+di.tagLabel))
	}
	rightParts = append(rightParts, dimStyle.Render(snap.ProviderID))
	if email := snapshotMeta(snap, "account_email"); email != "" {
		rightParts = append(rightParts, dimStyle.Render(email))
	}
	if planName := snapshotMeta(snap, "plan_name"); planName != "" {
		rightParts = append(rightParts, dimStyle.Render(planName))
	}
	rightParts = append(rightParts, StatusBadge(snap.Status))

	left := "  " + statusIcon + " " + name
	right := strings.Join(rightParts, dimStyle.Render(" · "))
	leftW := lipgloss.Width(left)
	rightW := lipgloss.Width(right)
	gap := w - leftW - rightW - 1
	if gap < 1 {
		gap = 1
	}
	sb.WriteString(left + strings.Repeat(" ", gap) + right + "\n")

	// Line 2: summary info (left) ... timestamp (right)
	var summaryParts []string
	if di.summary != "" {
		summaryParts = append(summaryParts, lipgloss.NewStyle().Bold(true).Foreground(colorText).Render(di.summary))
	}
	if di.detail != "" {
		summaryParts = append(summaryParts, dimStyle.Render(di.detail))
	}
	if snap.Message != "" && di.summary == "" {
		summaryParts = append(summaryParts, lipgloss.NewStyle().Italic(true).Foreground(colorSubtext).Render(snap.Message))
	}
	summaryLeft := "  " + strings.Join(summaryParts, dimStyle.Render("  ·  "))

	timeStr := snap.Timestamp.Format("15:04:05")
	age := time.Since(snap.Timestamp)
	if age > 60*time.Second {
		timeStr = fmt.Sprintf("%s (%s ago)", snap.Timestamp.Format("15:04:05"), formatDuration(age))
	}
	summaryRight := dimStyle.Render("⏱ " + timeStr)
	sLeftW := lipgloss.Width(summaryLeft)
	sRightW := lipgloss.Width(summaryRight)
	sGap := w - sLeftW - sRightW - 1
	if sGap < 1 {
		sGap = 1
	}
	sb.WriteString(summaryLeft + strings.Repeat(" ", sGap) + summaryRight + "\n")

	// Accent separator colored by status.
	sepColor := StatusBorderColor(snap.Status)
	sepLen := w - 2
	if sepLen < 4 {
		sepLen = 4
	}
	sb.WriteString(" " + lipgloss.NewStyle().Foreground(sepColor).Render(strings.Repeat("━", sepLen)) + "\n")
}

// ── Bordered Card Sections ─────────────────────────────────────────────────
// Each section is rendered inside a bordered card with a title in the top border.

func renderDetailCard(sb *strings.Builder, sec detailSection, w int) {
	if len(sec.lines) == 0 {
		return
	}

	cardW := w - 4 // outer margins
	if cardW < 30 {
		cardW = 30
	}
	innerW := cardW - 4 // border + padding

	color := sec.color
	if color == "" {
		color = sectionColor(sec.title)
	}
	icon := sec.icon
	if icon == "" {
		icon = sectionIcon(sec.title)
	}

	sb.WriteString("\n")

	if sec.hasOwnHeader {
		// Composition sections already have their own styled heading.
		// Wrap in a subtle bordered card without a title in the border.
		topBorder := "  " + lipgloss.NewStyle().Foreground(colorSurface1).Render("╭"+strings.Repeat("─", cardW-2)+"╮")
		sb.WriteString(topBorder + "\n")

		for _, line := range sec.lines {
			// Pad each line to fit inside the card.
			lineW := lipgloss.Width(line)
			pad := innerW - lineW
			if pad < 0 {
				pad = 0
			}
			sb.WriteString("  " + lipgloss.NewStyle().Foreground(colorSurface1).Render("│") + " " + line + strings.Repeat(" ", pad) + " " + lipgloss.NewStyle().Foreground(colorSurface1).Render("│") + "\n")
		}

		botBorder := "  " + lipgloss.NewStyle().Foreground(colorSurface1).Render("╰"+strings.Repeat("─", cardW-2)+"╯")
		sb.WriteString(botBorder + "\n")
		return
	}

	// Build card with title embedded in the top border.
	titleStr := " " + icon + " " + sec.title + " "
	titleRendered := lipgloss.NewStyle().Foreground(color).Bold(true).Render(titleStr)
	titleW := lipgloss.Width(titleRendered)

	// Top border: ╭─ Title ─────────────────╮
	leftBorderLen := 1 // after ╭
	rightBorderLen := cardW - 2 - leftBorderLen - titleW
	if rightBorderLen < 1 {
		rightBorderLen = 1
	}
	topBorder := "  " +
		lipgloss.NewStyle().Foreground(color).Render("╭"+strings.Repeat("─", leftBorderLen)) +
		titleRendered +
		lipgloss.NewStyle().Foreground(color).Render(strings.Repeat("─", rightBorderLen)+"╮")
	sb.WriteString(topBorder + "\n")

	// Body lines.
	borderChar := lipgloss.NewStyle().Foreground(color).Render("│")
	for _, line := range sec.lines {
		lineW := lipgloss.Width(line)
		pad := innerW - lineW
		if pad < 0 {
			pad = 0
		}
		sb.WriteString("  " + borderChar + " " + line + strings.Repeat(" ", pad) + " " + borderChar + "\n")
	}

	// Bottom border.
	botBorder := "  " + lipgloss.NewStyle().Foreground(color).Render("╰"+strings.Repeat("─", cardW-2)+"╯")
	sb.WriteString(botBorder + "\n")
}
