package analysis

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/ArchipelagoAI/health-reporter/pkg/types"
)

type Config struct {
	WindowHours      int
	AnomalyThreshold float64
	MinDataPoints    int
	LLMEnabled       bool
	LLMEndpoint      string
	LLMModel         string
	LLMTimeout       int
}

type TrendDetector struct {
	windowHours      int
	anomalyThreshold float64
	minDataPoints    int
}

type TrendResult struct {
	Direction     string  `json:"direction"`
	ChangePercent float64 `json:"change_percent"`
	Description   string  `json:"description"`
	Severity      string  `json:"severity"`
}

type Anomaly struct {
	Type        string  `json:"type"`
	Severity    string  `json:"severity"`
	Description string  `json:"description"`
	Confidence  float64 `json:"confidence"`
}

type Prediction struct {
	Type                  string `json:"type"`
	RiskLevel             string `json:"risk_level"`
	EstimatedHoursToEvent int    `json:"estimated_hours_to_saturation"`
	Description           string `json:"description"`
}

type AnalysisResult struct {
	Timestamp       time.Time              `json:"timestamp"`
	Trends          map[string]TrendResult `json:"trends"`
	Anomalies       []Anomaly              `json:"anomalies"`
	Predictions     []Prediction           `json:"predictions"`
	Recommendations []types.Recommendation `json:"recommendations"`
	HealthSummary   string                 `json:"health_summary"`
	AgentVersion    string                 `json:"agent_version"`
	ConfidenceScore float64                `json:"confidence_score"`
}

func NewTrendDetector(windowHours int, anomalyThreshold float64, minDataPoints int) *TrendDetector {
	return &TrendDetector{
		windowHours:      windowHours,
		anomalyThreshold: anomalyThreshold,
		minDataPoints:    minDataPoints,
	}
}

func (t *TrendDetector) Analyze(ctx context.Context, currentReport *types.Report, historicalReports []*types.Report) *AnalysisResult {
	result := &AnalysisResult{
		Timestamp:       time.Now().UTC(),
		Trends:          make(map[string]TrendResult),
		Anomalies:       []Anomaly{},
		Predictions:     []Prediction{},
		Recommendations: []types.Recommendation{},
		AgentVersion:    "1.0.0",
	}

	if len(historicalReports) < t.minDataPoints {
		result.HealthSummary = "Insufficient historical data for trend analysis"
		result.ConfidenceScore = 0.0
		return result
	}

	result.Trends = t.calculateTrends(currentReport, historicalReports)
	result.Anomalies = t.detectAnomalies(currentReport, historicalReports)
	result.Predictions = t.predictIssues(currentReport, historicalReports)
	result.HealthSummary = t.generateSummary(currentReport, result.Trends)
	result.ConfidenceScore = t.calculateConfidence(len(historicalReports))

	return result
}

func (t *TrendDetector) calculateTrends(currentReport *types.Report, historicalReports []*types.Report) map[string]TrendResult {
	trends := make(map[string]TrendResult)

	avgCPU := t.calculateAverage(historicalReports, "cpu")
	avgMemory := t.calculateAverage(historicalReports, "memory")
	avgRestarts := t.calculateAverageRestarts(historicalReports)

	currentCPU := getResourceUsage(currentReport, "cpu")
	currentMemory := getResourceUsage(currentReport, "memory")
	currentRestarts := getPodRestarts(currentReport)

	if avgCPU > 0 {
		changePercent := ((currentCPU - avgCPU) / avgCPU) * 100
		trends["cpu"] = TrendResult{
			Direction:     t.getDirection(changePercent),
			ChangePercent: math.Round(changePercent*10) / 10,
			Description:   t.formatDescription("CPU", currentCPU, avgCPU),
			Severity:      t.getTrendSeverity(changePercent),
		}
	}

	if avgMemory > 0 {
		changePercent := ((currentMemory - avgMemory) / avgMemory) * 100
		trends["memory"] = TrendResult{
			Direction:     t.getDirection(changePercent),
			ChangePercent: math.Round(changePercent*10) / 10,
			Description:   t.formatDescription("Memory", currentMemory, avgMemory),
			Severity:      t.getTrendSeverity(changePercent),
		}
	}

	if avgRestarts > 0 {
		changePercent := ((float64(currentRestarts) - avgRestarts) / avgRestarts) * 100
		trends["pod_restarts"] = TrendResult{
			Direction:     t.getDirection(changePercent),
			ChangePercent: math.Round(changePercent*10) / 10,
			Description:   t.formatDescription("Pod restarts", float64(currentRestarts), avgRestarts),
			Severity:      t.getTrendSeverity(changePercent),
		}
	}

	return trends
}

func (t *TrendDetector) detectAnomalies(currentReport *types.Report, historicalReports []*types.Report) []Anomaly {
	var anomalies []Anomaly

	avgRestarts := t.calculateAverageRestarts(historicalReports)
	currentRestarts := getPodRestarts(currentReport)

	if avgRestarts > 0 && float64(currentRestarts) > avgRestarts*t.anomalyThreshold {
		anomalies = append(anomalies, Anomaly{
			Type:        "pod_restart_spike",
			Severity:    "medium",
			Description: currentReport.Summary,
			Confidence:  0.75,
		})
	}

	avgCPU := t.calculateAverage(historicalReports, "cpu")
	currentCPU := getResourceUsage(currentReport, "cpu")
	if avgCPU > 0 && currentCPU > avgCPU*t.anomalyThreshold {
		anomalies = append(anomalies, Anomaly{
			Type:        "cpu_spike",
			Severity:    "high",
			Description: "CPU usage significantly above baseline",
			Confidence:  0.85,
		})
	}

	return anomalies
}

func (t *TrendDetector) predictIssues(currentReport *types.Report, historicalReports []*types.Report) []Prediction {
	var predictions []Prediction

	currentCPU := getResourceUsage(currentReport, "cpu")
	if currentCPU > 70 {
		trends := t.calculateTrends(currentReport, historicalReports)
		if trend, ok := trends["cpu"]; ok && trend.Direction == "increasing" {
			hoursToSaturation := t.estimateHoursToSaturation(currentCPU, 90, trend.ChangePercent)
			if hoursToSaturation > 0 && hoursToSaturation < 72 {
				predictions = append(predictions, Prediction{
					Type:                  "resource_saturation",
					RiskLevel:             "low",
					EstimatedHoursToEvent: hoursToSaturation,
					Description:           "At current CPU trend, cluster will reach 90% CPU in ~" + formatHours(hoursToSaturation),
				})
			}
		}
	}

	return predictions
}

func (t *TrendDetector) generateSummary(report *types.Report, trends map[string]TrendResult) string {
	summary := "Cluster status: " + string(report.Status) + ". "

	for key, trend := range trends {
		if trend.Severity == "critical" || trend.Severity == "elevated" {
			summary += key + " is " + trend.Direction + " (" + formatPercent(trend.ChangePercent) + "). "
		}
	}

	return summary
}

func (t *TrendDetector) calculateConfidence(dataPoints int) float64 {
	confidence := float64(dataPoints) / 24.0
	if confidence > 1.0 {
		confidence = 1.0
	}
	return math.Round(confidence*100) / 100
}

func (t *TrendDetector) calculateAverage(reports []*types.Report, metric string) float64 {
	if len(reports) == 0 {
		return 0
	}

	var sum float64
	count := 0

	for _, r := range reports {
		if cm, ok := r.ClusterMetrics["resources"].(map[string]interface{}); ok {
			var value float64
			switch metric {
			case "cpu":
				if v, ok := cm["cpu_usage_percent"].(float64); ok {
					value = v
				} else if v, ok := cm["cpu_usage_percent"].(int); ok {
					value = float64(v)
				}
			case "memory":
				if v, ok := cm["memory_usage_percent"].(float64); ok {
					value = v
				} else if v, ok := cm["memory_usage_percent"].(int); ok {
					value = float64(v)
				}
			}
			sum += value
			count++
		}
	}

	if count == 0 {
		return 0
	}
	return sum / float64(count)
}

func (t *TrendDetector) calculateAverageRestarts(reports []*types.Report) float64 {
	if len(reports) == 0 {
		return 0
	}

	var sum float64
	count := 0

	for _, r := range reports {
		if cm, ok := r.ClusterMetrics["pods"].(map[string]interface{}); ok {
			var value float64
			if v, ok := cm["restarts"].(float64); ok {
				value = v
			} else if v, ok := cm["restarts"].(int); ok {
				value = float64(v)
			}
			sum += value
			count++
		}
	}

	if count == 0 {
		return 0
	}
	return sum / float64(count)
}

func (t *TrendDetector) getDirection(change float64) string {
	if change > 2 {
		return "increasing"
	} else if change < -2 {
		return "decreasing"
	}
	return "stable"
}

func (t *TrendDetector) getTrendSeverity(change float64) string {
	absChange := math.Abs(change)
	if absChange < 5 {
		return "stable"
	} else if absChange < 15 {
		return "moderate"
	} else if absChange < 30 {
		return "elevated"
	}
	return "critical"
}

func (t *TrendDetector) formatDescription(metric string, current, avg float64) string {
	direction := "stable"
	if current > avg {
		direction = "increasing"
	} else if current < avg {
		direction = "decreasing"
	}

	changePercent := ((current - avg) / avg) * 100
	return metric + " " + direction + " at " + formatPercent(current) + " (avg: " + formatPercent(avg) + ", change: " + formatPercent(changePercent) + ")"
}

func (t *TrendDetector) estimateHoursToSaturation(current, threshold, rate float64) int {
	if rate <= 0 {
		return 0
	}
	hours := (threshold - current) / rate
	return int(math.Round(hours))
}

func getResourceUsage(report *types.Report, metric string) float64 {
	if cm, ok := report.ClusterMetrics["resources"].(map[string]interface{}); ok {
		switch metric {
		case "cpu":
			if v, ok := cm["cpu_usage_percent"].(float64); ok {
				return v
			} else if v, ok := cm["cpu_usage_percent"].(int); ok {
				return float64(v)
			}
		case "memory":
			if v, ok := cm["memory_usage_percent"].(float64); ok {
				return v
			} else if v, ok := cm["memory_usage_percent"].(int); ok {
				return float64(v)
			}
		}
	}
	return 0
}

func getPodRestarts(report *types.Report) int {
	if cm, ok := report.ClusterMetrics["pods"].(map[string]interface{}); ok {
		if v, ok := cm["restarts"].(float64); ok {
			return int(v)
		} else if v, ok := cm["restarts"].(int); ok {
			return v
		}
	}
	return 0
}

func formatPercent(value float64) string {
	return fmt.Sprintf("%.1f%%", math.Round(value*10)/10)
}

func formatHours(hours int) string {
	if hours >= 24 {
		return fmt.Sprintf("%d days", hours/24)
	}
	return fmt.Sprintf("%d hours", hours)
}
