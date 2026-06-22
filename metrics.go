package main

import (
	"encoding/csv"
	"os"
	"strconv"
	"sync"
	"time"
)

type MetricRecord struct {
	Timestamp                time.Time
	RequestID                string
	TierUsed                 int
	LatencyMS                float64
	ActiveContainersPoolSize int
}

type MetricsLogger struct {
	file   *os.File
	writer *csv.Writer
	mu     sync.Mutex
}

func NewMetricsLogger(path string) (*MetricsLogger, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}

	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, err
	}

	logger := &MetricsLogger{
		file:   file,
		writer: csv.NewWriter(file),
	}

	if info.Size() == 0 {
		if err := logger.writer.Write([]string{
			"Timestamp",
			"RequestID",
			"TierUsed",
			"LatencyMS",
			"ActiveContainersPoolSize",
		}); err != nil {
			file.Close()
			return nil, err
		}
		logger.writer.Flush()
		if err := logger.writer.Error(); err != nil {
			file.Close()
			return nil, err
		}
	}

	return logger, nil
}

func (l *MetricsLogger) Write(record MetricRecord) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	row := []string{
		record.Timestamp.Format(time.RFC3339Nano),
		record.RequestID,
		strconv.Itoa(record.TierUsed),
		strconv.FormatFloat(record.LatencyMS, 'f', 3, 64),
		strconv.Itoa(record.ActiveContainersPoolSize),
	}

	if err := l.writer.Write(row); err != nil {
		return err
	}

	l.writer.Flush()
	return l.writer.Error()
}

func (l *MetricsLogger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.writer.Flush()
	err := l.writer.Error()
	if closeErr := l.file.Close(); err == nil {
		err = closeErr
	}
	return err
}
