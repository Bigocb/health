package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/ArchipelagoAI/health-reporter/pkg/types"
)

type HistoryManager struct {
	reportsDir     string
	retentionHours int
}

func NewHistoryManager(reportsDir string, retentionHours int) *HistoryManager {
	return &HistoryManager{
		reportsDir:     reportsDir,
		retentionHours: retentionHours,
	}
}

func (h *HistoryManager) EnsureReportsDir() error {
	return os.MkdirAll(h.reportsDir, 0o755)
}

func (h *HistoryManager) SaveReport(ctx context.Context, report *types.Report) error {
	if err := h.EnsureReportsDir(); err != nil {
		return fmt.Errorf("failed to create reports directory: %w", err)
	}

	filename := filepath.Join(h.reportsDir, fmt.Sprintf("%s.json", report.Timestamp.Format(time.RFC3339)))
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal report: %w", err)
	}

	if err := os.WriteFile(filename, data, 0o644); err != nil {
		return fmt.Errorf("failed to write report: %w", err)
	}

	return nil
}

func (h *HistoryManager) LoadReports(ctx context.Context, hours int) ([]*types.Report, error) {
	reports, err := h.loadAllReports()
	if err != nil {
		return nil, err
	}

	cutoff := time.Now().Add(-time.Duration(hours) * time.Hour)
	var filtered []*types.Report
	for _, r := range reports {
		if r.Timestamp.After(cutoff) {
			filtered = append(filtered, r)
		}
	}

	return filtered, nil
}

func (h *HistoryManager) loadAllReports() ([]*types.Report, error) {
	entries, err := os.ReadDir(h.reportsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []*types.Report{}, nil
		}
		return nil, fmt.Errorf("failed to read reports directory: %w", err)
	}

	var reports []*types.Report
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		filepath := filepath.Join(h.reportsDir, entry.Name())
		data, err := os.ReadFile(filepath)
		if err != nil {
			continue
		}

		var report types.Report
		if err := json.Unmarshal(data, &report); err != nil {
			continue
		}
		reports = append(reports, &report)
	}

	sort.Slice(reports, func(i, j int) bool {
		return reports[i].Timestamp.Before(reports[j].Timestamp)
	})

	return reports, nil
}

func (h *HistoryManager) CleanupOldReports(ctx context.Context) error {
	entries, err := os.ReadDir(h.reportsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to read reports directory: %w", err)
	}

	cutoff := time.Now().Add(-time.Duration(h.retentionHours) * time.Hour)
	var cleanupErrors []error

	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		filepath := filepath.Join(h.reportsDir, entry.Name())
		info, err := os.Stat(filepath)
		if err != nil {
			continue
		}

		if info.ModTime().Before(cutoff) {
			if err := os.Remove(filepath); err != nil {
				cleanupErrors = append(cleanupErrors, err)
			}
		}
	}

	if len(cleanupErrors) > 0 {
		return fmt.Errorf("failed to cleanup some reports: %v", cleanupErrors)
	}

	return nil
}

func (h *HistoryManager) GetReportCount() (int, error) {
	entries, err := os.ReadDir(h.reportsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}

	count := 0
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) == ".json" {
			count++
		}
	}
	return count, nil
}
