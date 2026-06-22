package main

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	Port           string
	MetricsFile    string
	ImageName      string
	TaskDurationMS int
	EvictionAfter  time.Duration
	SweepInterval  time.Duration
	RequestTimeout time.Duration
	DockerTimeout  time.Duration
	AutoWarmStart  bool
	WarmOnInvoke   bool
}

func LoadConfig() Config {
	return Config{
		Port:           getEnv("PORT", "8080"),
		MetricsFile:    getEnv("METRICS_FILE", "metrics.csv"),
		ImageName:      getEnv("FAAS_IMAGE", "multitier-faas-mock:latest"),
		TaskDurationMS: getEnvInt("TASK_DURATION_MS", 1000),
		EvictionAfter:  time.Duration(getEnvInt("EVICTION_SECONDS", 30)) * time.Second,
		SweepInterval:  time.Duration(getEnvInt("EVICTION_SWEEP_SECONDS", 5)) * time.Second,
		RequestTimeout: time.Duration(getEnvInt("REQUEST_TIMEOUT_SECONDS", 15)) * time.Second,
		DockerTimeout:  time.Duration(getEnvInt("DOCKER_TIMEOUT_SECONDS", 60)) * time.Second,
		AutoWarmStart:  getEnvBool("AUTO_WARM_START", true),
		WarmOnInvoke:   getEnvBool("WARM_ON_INVOKE", true),
	}
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func getEnvBool(key string, fallback bool) bool {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}

	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}
