package analysis

// ResourceThreshold defines what constitutes good, elevated, and critical resource usage
type ResourceThreshold struct {
	Good     int // Percentage: usage below this is healthy
	Elevated int // Percentage: usage between Good and Elevated is concerning
	Critical int // Percentage: usage above this is critical
}

// HealthThresholds contains all thresholds for evaluating cluster health
type HealthThresholds struct {
	CPU    ResourceThreshold // CPU usage: good <70%, elevated 70-85%, critical >85%
	Memory ResourceThreshold // Memory usage: good <75%, elevated 75-90%, critical >90%
	Disk   ResourceThreshold // Disk usage: good <80%, elevated 80-95%, critical >95%
}

// DefaultThresholds returns sensible defaults for Kubernetes cluster health evaluation
func DefaultThresholds() HealthThresholds {
	return HealthThresholds{
		CPU: ResourceThreshold{
			Good:     70,
			Elevated: 85,
			Critical: 100,
		},
		Memory: ResourceThreshold{
			Good:     75,
			Elevated: 90,
			Critical: 100,
		},
		Disk: ResourceThreshold{
			Good:     80,
			Elevated: 95,
			Critical: 100,
		},
	}
}

// EvaluateStatus returns "good", "elevated", or "critical" based on value and threshold
func (rt ResourceThreshold) EvaluateStatus(value float64) string {
	if value < float64(rt.Good) {
		return "good"
	}
	if value < float64(rt.Elevated) {
		return "elevated"
	}
	return "critical"
}
