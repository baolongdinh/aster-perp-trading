package notifier

import (
	"bytes"
	"fmt"
	"math"
	"text/template"
	"time"
)

// The main template matching the spec aesthetics.
// Using HTML tags for simple bolding/italics.
const reportTemplateStr = `📊 <b>Grid Bot Report</b> — {{.Time}}

💱 <b>{{.Symbol}}</b> | Giá: {{printf "%.4f" .CurrentPrice}}
📍 Vị trí trong grid: {{.GridBar}} {{printf "%.1f" .GridPct}}% ↔️ {{.GridPositionText}}

──────────────────
🟢 <b>PnL</b>
  Realized  : {{printf "%+0.4f" .RealizedPnL}} USDT
  Unrealized: {{printf "%+0.4f" .UnrealizedPnL}} USDT
  Fees paid : {{printf "%.4f" .FeesPaid}} USDT
  Net PnL   : <b>{{printf "%+0.4f" .NetPnL}} USDT</b>

📈 <b>Volume (30m)</b>
  Volume  : {{printf "%.2f" .Volume30m}} USDT
  Filled  : {{.FilledOrders30m}} lệnh
  Pending : {{.PendingOrders}} lệnh

🔲 <b>Grid Status</b>
  Range   : {{printf "%.4f" .GridMinPrice}} → {{printf "%.4f" .GridMaxPrice}}
  Active  : {{.ActiveGrids}} / {{.TotalGrids}} grids
  Last ord: {{.LastOrderMinutes}} phút trước

💼 <b>Balance</b>
  Vốn ban đầu: {{printf "%.2f" .InitialCapital}} USDT
  Hiện tại   : <b>{{printf "%.2f" .CurrentCapital}} USDT</b>
  ROI        : {{printf "%+0.2f" .ROI}}%

⏱ Uptime: {{.UptimeFormat}} | Drawdown: {{printf "%.2f" .DrawdownPct}}%`

var reportTmpl = template.Must(template.New("report").Parse(reportTemplateStr))

// templateData handles mappings for the formatter.
type templateData struct {
	Time             string
	Symbol           string
	CurrentPrice     float64
	GridBar          string
	GridPct          float64
	GridPositionText string

	RealizedPnL   float64
	UnrealizedPnL float64
	FeesPaid      float64
	NetPnL        float64

	Volume30m       float64
	FilledOrders30m int
	PendingOrders   int

	GridMinPrice     float64
	GridMaxPrice     float64
	ActiveGrids      int
	TotalGrids       int
	LastOrderMinutes string

	InitialCapital float64
	CurrentCapital float64
	ROI            float64

	UptimeFormat string
	DrawdownPct  float64
}

// FormatPeriodicReport produces the standard 30m grid update message
func FormatPeriodicReport(metrics GridMetrics) (string, error) {
	data := generateTemplateData(metrics)
	var buf bytes.Buffer
	if err := reportTmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// FormatStartup generates an initialization message.
func FormatStartup(symbol string, minPrice, maxPrice float64, totalGrids int) string {
	return fmt.Sprintf("🚀 <b>Grid Bot Startup</b>\n\n💱 Symbol: <b>%s</b>\n🔲 Range: %.4f → %.4f\n🔢 Grids: %d\n\nBot is now actively scanning the market.",
		symbol, minPrice, maxPrice, totalGrids)
}

// FormatError generating panic/error messages.
func FormatError(err error) string {
	return fmt.Sprintf("❌ <b>BOT ERROR</b>\n\nLỗi nghiêm trọng xảy ra:\n<pre>%s</pre>\n\nVui lòng kiểm tra server!", err.Error())
}

// FormatAlert generic alert message formatting.
func FormatAlert(alertType, description string) string {
	var emoji string
	switch alertType {
	case "Drawdown":
		emoji = "🚨"
	case "Breakout":
		emoji = "🚨"
	case "NoOrders":
		emoji = "⚠️"
	case "Volatility":
		emoji = "⚡"
	case "Shutdown":
		emoji = "🛑"
	default:
		emoji = "🔔"
	}
	return fmt.Sprintf("%s <b>%s Alert</b>\n\n%s", emoji, alertType, description)
}

func generateTemplateData(m GridMetrics) templateData {
	now := time.Now()

	// Format Grid Bar
	gridPct := 0.0
	if m.GridMaxPrice > m.GridMinPrice && m.CurrentPrice >= m.GridMinPrice {
		if m.CurrentPrice > m.GridMaxPrice {
			gridPct = 100.0
		} else {
			gridPct = (m.CurrentPrice - m.GridMinPrice) / (m.GridMaxPrice - m.GridMinPrice) * 100
		}
	} else if m.GridMaxPrice == 0 && m.GridMinPrice == 0 {
		gridPct = 50.0 // fallback if no grid bounding box is established
	}

	barBlocks := int(math.Round(gridPct / 10))
	if barBlocks > 10 {
		barBlocks = 10
	}
	if barBlocks < 0 {
		barBlocks = 0
	}

	gridBar := ""
	for i := 0; i < 10; i++ {
		if i < barBlocks {
			gridBar += "█"
		} else {
			gridBar += "░"
		}
	}

	gridPosTxt := "giữa grid"
	if gridPct < 15 {
		gridPosTxt = "đáy grid"
	} else if gridPct > 85 {
		gridPosTxt = "đỉnh grid"
	}

	lastOrdMins := "0.0"
	if !m.LastOrderTime.IsZero() {
		lastOrdMins = fmt.Sprintf("%.1f", time.Since(m.LastOrderTime).Minutes())
	}

	uptimeStr := m.Uptime.Round(time.Minute).String()
	// Reformat for short hours/mins (e.g., "2h30m0s" -> "2h30m")
	if len(uptimeStr) > 0 && uptimeStr[len(uptimeStr)-2:] == "0s" {
		uptimeStr = uptimeStr[:len(uptimeStr)-2]
	}

	return templateData{
		Time:             now.Format("02/01 15:04"),
		Symbol:           m.Symbol,
		CurrentPrice:     m.CurrentPrice,
		GridBar:          fmt.Sprintf("[%s]", gridBar),
		GridPct:          gridPct,
		GridPositionText: gridPosTxt,

		RealizedPnL:   m.RealizedPnL,
		UnrealizedPnL: m.UnrealizedPnL,
		FeesPaid:      m.FeesPaid,
		NetPnL:        m.NetPnL,

		Volume30m:       m.Volume30m,
		FilledOrders30m: m.FilledOrders30m,
		PendingOrders:   m.PendingOrders,

		GridMinPrice:     m.GridMinPrice,
		GridMaxPrice:     m.GridMaxPrice,
		ActiveGrids:      m.ActiveGrids,
		TotalGrids:       m.TotalGrids,
		LastOrderMinutes: lastOrdMins,

		InitialCapital: m.InitialCapital,
		CurrentCapital: m.CurrentCapital,
		ROI:            m.ROI,

		UptimeFormat: uptimeStr,
		DrawdownPct:  m.DrawdownPct,
	}
}
