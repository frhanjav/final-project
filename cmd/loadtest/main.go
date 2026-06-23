package main

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"sort"
	"strconv"
	"sync"
	"time"
)

type invokeRequest struct {
	RequestID  string `json:"request_id"`
	DurationMS int    `json:"duration_ms,omitempty"`
	ForceTier  *int   `json:"force_tier,omitempty"`
}

type invokeResponse struct {
	RequestID                string  `json:"request_id"`
	TierUsed                 int     `json:"tier_used"`
	LatencyMS                float64 `json:"latency_ms"`
	ActiveContainersPoolSize int     `json:"active_containers_pool_size"`
	Message                  string  `json:"message"`
}

type requestResult struct {
	LatencyMS  float64
	StatusCode int
	Err        string
	TierUsed   *int
}

type summary struct {
	Timestamp     time.Time
	RunLabel      string
	Endpoint      string
	TotalRequests int
	Success       int
	Errors        int
	TargetRPS     int
	Concurrency   int
	DurationMS    int
	ForceTier     string
	ObservedRPS   float64
	AvgLatencyMS  float64
	MinLatencyMS  float64
	MaxLatencyMS  float64
	P50LatencyMS  float64
	P95LatencyMS  float64
	P99LatencyMS  float64
	Tier0Count    int
	Tier1Count    int
	Tier2Count    int
	ErrorSamples  []string
}

func main() {
	var (
		endpoint    = flag.String("endpoint", "http://localhost:8080/invoke", "invoke endpoint")
		label       = flag.String("label", "load-run", "label written to the summary CSV")
		total       = flag.Int("total", 0, "total number of requests to send; if zero, derived from -seconds and -rps")
		seconds     = flag.Int("seconds", 0, "duration of the run in seconds when deriving total requests")
		rps         = flag.Int("rps", 100, "target requests per second")
		concurrency = flag.Int("concurrency", 32, "number of in-flight worker goroutines")
		durationMS  = flag.Int("duration-ms", 1000, "task duration to send in the request payload")
		forceTier   = flag.Int("force-tier", -1, "force a tier for every request; use -1 for automatic routing")
		timeout     = flag.Duration("timeout", 30*time.Second, "per-request HTTP timeout")
		output      = flag.String("output", "loadtest_summary.csv", "summary CSV file path")
	)
	flag.Parse()

	if *total <= 0 {
		if *seconds > 0 && *rps > 0 {
			*total = *seconds * *rps
		} else {
			*total = 100
		}
	}
	if *concurrency <= 0 {
		fmt.Fprintln(os.Stderr, "concurrency must be greater than zero")
		os.Exit(1)
	}

	client := &http.Client{Timeout: *timeout}
	jobs := make(chan int)
	results := make(chan requestResult, *total)

	var wg sync.WaitGroup
	for range *concurrency {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				results <- sendRequest(client, *endpoint, *label, i, *durationMS, *forceTier)
			}
		}()
	}

	start := time.Now()

	go func() {
		defer close(jobs)
		if *rps <= 0 {
			for i := range *total {
				jobs <- i
			}
			return
		}

		interval := time.Second / time.Duration(*rps)
		if interval <= 0 {
			interval = time.Nanosecond
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for i := range *total {
			<-ticker.C
			jobs <- i
		}
	}()

	go func() {
		wg.Wait()
		close(results)
	}()

	summary := collectSummary(results, *label, *endpoint, *total, *rps, *concurrency, *durationMS, *forceTier, start)
	if err := appendSummary(*output, summary); err != nil {
		fmt.Fprintf(os.Stderr, "write summary CSV: %v\n", err)
		os.Exit(1)
	}

	printSummary(summary, *output)
}

func sendRequest(client *http.Client, endpoint, label string, index, durationMS, forceTier int) requestResult {
	reqBody := invokeRequest{
		RequestID:  fmt.Sprintf("%s-%05d", label, index),
		DurationMS: durationMS,
	}
	if forceTier >= 0 {
		reqBody.ForceTier = &forceTier
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return requestResult{Err: err.Error()}
	}

	start := time.Now()
	resp, err := client.Post(endpoint, "application/json", bytes.NewReader(body))
	latencyMS := float64(time.Since(start).Microseconds()) / 1000.0
	if err != nil {
		return requestResult{LatencyMS: latencyMS, Err: err.Error()}
	}
	defer resp.Body.Close()

	result := requestResult{
		LatencyMS:  latencyMS,
		StatusCode: resp.StatusCode,
	}

	bodyBytes, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		result.Err = readErr.Error()
		return result
	}

	var decoded invokeResponse
	if err := json.Unmarshal(bodyBytes, &decoded); err == nil {
		tier := decoded.TierUsed
		result.TierUsed = &tier
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bodyText := string(bytes.TrimSpace(bodyBytes))
		if bodyText == "" {
			bodyText = http.StatusText(resp.StatusCode)
		}
		result.Err = fmt.Sprintf("status %d: %s", resp.StatusCode, bodyText)
	}
	return result
}

func collectSummary(results <-chan requestResult, label, endpoint string, total, rps, concurrency, durationMS, forceTier int, start time.Time) summary {
	s := summary{
		Timestamp:     time.Now(),
		RunLabel:      label,
		Endpoint:      endpoint,
		TotalRequests: total,
		TargetRPS:     rps,
		Concurrency:   concurrency,
		DurationMS:    durationMS,
		ForceTier:     "auto",
	}
	if forceTier >= 0 {
		s.ForceTier = strconv.Itoa(forceTier)
	}

	var latencies []float64
	for result := range results {
		if result.Err != "" {
			s.Errors++
			if len(s.ErrorSamples) < 5 {
				s.ErrorSamples = append(s.ErrorSamples, result.Err)
			}
			continue
		}

		s.Success++
		latencies = append(latencies, result.LatencyMS)
		if result.TierUsed != nil {
			switch *result.TierUsed {
			case 0:
				s.Tier0Count++
			case 1:
				s.Tier1Count++
			case 2:
				s.Tier2Count++
			}
		}
	}

	seconds := time.Since(start).Seconds()
	if seconds > 0 {
		s.ObservedRPS = float64(s.Success+s.Errors) / seconds
	}

	if len(latencies) == 0 {
		return s
	}

	sort.Float64s(latencies)
	s.MinLatencyMS = latencies[0]
	s.MaxLatencyMS = latencies[len(latencies)-1]
	s.P50LatencyMS = percentile(latencies, 50)
	s.P95LatencyMS = percentile(latencies, 95)
	s.P99LatencyMS = percentile(latencies, 99)

	var totalLatency float64
	for _, latency := range latencies {
		totalLatency += latency
	}
	s.AvgLatencyMS = totalLatency / float64(len(latencies))

	return s
}

func percentile(values []float64, p float64) float64 {
	if len(values) == 0 {
		return 0
	}
	if len(values) == 1 {
		return values[0]
	}

	position := (p / 100.0) * float64(len(values)-1)
	lower := int(math.Floor(position))
	upper := int(math.Ceil(position))
	if lower == upper {
		return values[lower]
	}

	weight := position - float64(lower)
	return values[lower] + (values[upper]-values[lower])*weight
}

func appendSummary(path string, s summary) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return err
	}

	writer := csv.NewWriter(file)
	if info.Size() == 0 {
		if err := writer.Write([]string{
			"Timestamp",
			"RunLabel",
			"Endpoint",
			"TotalRequests",
			"Success",
			"Errors",
			"TargetRPS",
			"Concurrency",
			"DurationMS",
			"ForceTier",
			"ObservedRPS",
			"AvgLatencyMS",
			"MinLatencyMS",
			"MaxLatencyMS",
			"P50LatencyMS",
			"P95LatencyMS",
			"P99LatencyMS",
			"Tier0Count",
			"Tier1Count",
			"Tier2Count",
		}); err != nil {
			return err
		}
	}

	row := []string{
		s.Timestamp.Format(time.RFC3339Nano),
		s.RunLabel,
		s.Endpoint,
		strconv.Itoa(s.TotalRequests),
		strconv.Itoa(s.Success),
		strconv.Itoa(s.Errors),
		strconv.Itoa(s.TargetRPS),
		strconv.Itoa(s.Concurrency),
		strconv.Itoa(s.DurationMS),
		s.ForceTier,
		fmt.Sprintf("%.2f", s.ObservedRPS),
		fmt.Sprintf("%.3f", s.AvgLatencyMS),
		fmt.Sprintf("%.3f", s.MinLatencyMS),
		fmt.Sprintf("%.3f", s.MaxLatencyMS),
		fmt.Sprintf("%.3f", s.P50LatencyMS),
		fmt.Sprintf("%.3f", s.P95LatencyMS),
		fmt.Sprintf("%.3f", s.P99LatencyMS),
		strconv.Itoa(s.Tier0Count),
		strconv.Itoa(s.Tier1Count),
		strconv.Itoa(s.Tier2Count),
	}
	if err := writer.Write(row); err != nil {
		return err
	}
	writer.Flush()
	return writer.Error()
}

func printSummary(s summary, output string) {
	fmt.Printf("run_label: %s\n", s.RunLabel)
	fmt.Printf("requests: %d success / %d errors\n", s.Success, s.Errors)
	fmt.Printf("target_rps: %d observed_rps: %.2f\n", s.TargetRPS, s.ObservedRPS)
	fmt.Printf("concurrency: %d force_tier: %s duration_ms: %d\n", s.Concurrency, s.ForceTier, s.DurationMS)
	fmt.Printf("latency_ms avg=%.3f p50=%.3f p95=%.3f p99=%.3f min=%.3f max=%.3f\n",
		s.AvgLatencyMS, s.P50LatencyMS, s.P95LatencyMS, s.P99LatencyMS, s.MinLatencyMS, s.MaxLatencyMS)
	fmt.Printf("tier_counts: tier0=%d tier1=%d tier2=%d\n", s.Tier0Count, s.Tier1Count, s.Tier2Count)
	if len(s.ErrorSamples) > 0 {
		fmt.Printf("error_samples: %v\n", s.ErrorSamples)
	}
	fmt.Printf("summary_csv: %s\n", output)
}
