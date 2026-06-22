package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

type managedContainer struct {
	ID       string
	Name     string
	Tier     int
	LastUsed time.Time
	Busy     bool
	Paused   bool
}

type InvokeResult struct {
	RequestID                string
	TierUsed                 int
	LatencyMS                float64
	ActiveContainersPoolSize int
}

type PoolManager struct {
	cfg     Config
	cli     *client.Client
	metrics *MetricsLogger

	mu                sync.Mutex
	tier1             *managedContainer
	tier2             *managedContainer
	tier1Provisioning bool
	tier2Provisioning bool
	requestSeq        atomic.Uint64
}

func NewPoolManager(ctx context.Context, cfg Config, metrics *MetricsLogger) (*PoolManager, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, err
	}

	manager := &PoolManager{
		cfg:     cfg,
		cli:     cli,
		metrics: metrics,
	}

	if err := manager.ensureImage(ctx); err != nil {
		cli.Close()
		return nil, err
	}

	if cfg.AutoWarmStart {
		if err := manager.ensureTier1(ctx); err != nil {
			cli.Close()
			return nil, err
		}
		if err := manager.ensureTier2(ctx); err != nil {
			cli.Close()
			return nil, err
		}
	}

	return manager, nil
}

func (m *PoolManager) Close(ctx context.Context) error {
	m.mu.Lock()
	tier1 := m.tier1
	tier2 := m.tier2
	m.tier1 = nil
	m.tier2 = nil
	m.mu.Unlock()

	for _, ctr := range []*managedContainer{tier2, tier1} {
		if ctr == nil {
			continue
		}
		if err := m.destroyManagedContainer(ctx, ctr); err != nil {
			log.Printf("cleanup %s: %v", ctr.Name, err)
		}
	}

	return m.cli.Close()
}

func (m *PoolManager) StartEvictionWorker(ctx context.Context) {
	ticker := time.NewTicker(m.cfg.SweepInterval)

	go func() {
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.evictIdle(ctx)
			}
		}
	}()
}

func (m *PoolManager) Invoke(ctx context.Context, req InvokeRequest) (InvokeResult, error) {
	requestID := req.RequestID
	if requestID == "" {
		requestID = m.nextRequestID()
	}

	durationMS := req.DurationMS
	if durationMS <= 0 {
		durationMS = m.cfg.TaskDurationMS
	}

	tier, release, err := m.acquireTier(req.ForceTier)
	if err != nil {
		return InvokeResult{}, err
	}
	if release != nil {
		defer release()
	}

	start := time.Now()
	runErr := m.runTier(ctx, tier, durationMS)
	if runErr != nil && tier != 0 && req.ForceTier == nil {
		log.Printf("tier %d failed, falling back to cold start: %v", tier, runErr)
		m.invalidateTier(tier)

		tier = 0
		runErr = m.runTier(ctx, tier, durationMS)
	}
	if runErr != nil {
		return InvokeResult{}, runErr
	}

	m.markTierUsed(tier)

	if m.cfg.WarmOnInvoke {
		go func() {
			bgCtx, cancel := context.WithTimeout(context.Background(), m.cfg.DockerTimeout)
			defer cancel()
			if err := m.reconcileWarmPool(bgCtx); err != nil {
				log.Printf("reconcile warm pool: %v", err)
			}
		}()
	}

	latencyMS := float64(time.Since(start).Microseconds()) / 1000.0
	poolSize := m.ActivePoolSize()

	record := MetricRecord{
		Timestamp:                time.Now(),
		RequestID:                requestID,
		TierUsed:                 tier,
		LatencyMS:                latencyMS,
		ActiveContainersPoolSize: poolSize,
	}
	if err := m.metrics.Write(record); err != nil {
		log.Printf("write metrics: %v", err)
	}

	return InvokeResult{
		RequestID:                requestID,
		TierUsed:                 tier,
		LatencyMS:                latencyMS,
		ActiveContainersPoolSize: poolSize,
	}, nil
}

func (m *PoolManager) ActivePoolSize() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	size := 0
	if m.tier1 != nil {
		size++
	}
	if m.tier2 != nil {
		size++
	}
	return size
}

func (m *PoolManager) acquireTier(forceTier *int) (int, func(), error) {
	m.mu.Lock()

	chooseTier := func(tier int, ctr *managedContainer) (int, func(), error) {
		if ctr == nil {
			m.mu.Unlock()
			return 0, nil, fmt.Errorf("tier %d is not available", tier)
		}
		if ctr.Busy {
			m.mu.Unlock()
			return 0, nil, fmt.Errorf("tier %d is currently busy", tier)
		}

		ctr.Busy = true
		return tier, func() {
			m.mu.Lock()
			ctr.Busy = false
			m.mu.Unlock()
		}, nil
	}

	if forceTier != nil {
		switch *forceTier {
		case 0:
			m.mu.Unlock()
			return 0, nil, nil
		case 1:
			return chooseTier(1, m.tier1)
		case 2:
			return chooseTier(2, m.tier2)
		default:
			m.mu.Unlock()
			return 0, nil, fmt.Errorf("force_tier must be 0, 1, or 2")
		}
	}

	if m.tier2 != nil && !m.tier2.Busy {
		return chooseTier(2, m.tier2)
	}
	if m.tier1 != nil && !m.tier1.Busy {
		return chooseTier(1, m.tier1)
	}

	m.mu.Unlock()
	return 0, nil, nil
}

func (m *PoolManager) runTier(ctx context.Context, tier int, durationMS int) error {
	switch tier {
	case 0:
		return m.runColdStart(ctx, durationMS)
	case 1:
		return m.runWarmExec(ctx, durationMS)
	case 2:
		return m.runHotExec(ctx, durationMS)
	default:
		return fmt.Errorf("unknown tier %d", tier)
	}
}

func (m *PoolManager) runColdStart(ctx context.Context, durationMS int) error {
	opCtx, cancel := context.WithTimeout(ctx, m.cfg.DockerTimeout)
	defer cancel()

	resp, err := m.cli.ContainerCreate(
		opCtx,
		&container.Config{
			Image: m.cfg.ImageName,
			Cmd:   []string{"python", "/app/task.py", strconv.Itoa(durationMS)},
			Tty:   false,
		},
		nil,
		nil,
		nil,
		fmt.Sprintf("faas-tier0-%d", time.Now().UnixNano()),
	)
	if err != nil {
		return err
	}

	remove := func() error {
		return m.cli.ContainerRemove(context.Background(), resp.ID, types.ContainerRemoveOptions{Force: true})
	}
	defer func() {
		if err := remove(); err != nil {
			log.Printf("remove cold container %s: %v", resp.ID, err)
		}
	}()

	if err := m.cli.ContainerStart(opCtx, resp.ID, types.ContainerStartOptions{}); err != nil {
		return err
	}

	return m.waitForContainerExit(opCtx, resp.ID)
}

func (m *PoolManager) runWarmExec(ctx context.Context, durationMS int) error {
	m.mu.Lock()
	ctr := m.tier1
	m.mu.Unlock()
	if ctr == nil {
		return errors.New("tier 1 container is not available")
	}

	return m.execTaskInContainer(ctx, ctr.ID, durationMS)
}

func (m *PoolManager) runHotExec(ctx context.Context, durationMS int) error {
	m.mu.Lock()
	ctr := m.tier2
	m.mu.Unlock()
	if ctr == nil {
		return errors.New("tier 2 container is not available")
	}

	opCtx, cancel := context.WithTimeout(ctx, m.cfg.DockerTimeout)
	defer cancel()

	if err := m.cli.ContainerUnpause(opCtx, ctr.ID); err != nil {
		return err
	}

	if err := m.execTaskInContainer(opCtx, ctr.ID, durationMS); err != nil {
		return err
	}

	if err := m.cli.ContainerPause(opCtx, ctr.ID); err != nil {
		return err
	}

	m.mu.Lock()
	if m.tier2 != nil && m.tier2.ID == ctr.ID {
		m.tier2.Paused = true
	}
	m.mu.Unlock()

	return nil
}

func (m *PoolManager) execTaskInContainer(ctx context.Context, containerID string, durationMS int) error {
	opCtx, cancel := context.WithTimeout(ctx, m.cfg.DockerTimeout)
	defer cancel()

	execResp, err := m.cli.ContainerExecCreate(opCtx, containerID, types.ExecConfig{
		Cmd:          []string{"python", "/app/task.py", strconv.Itoa(durationMS)},
		AttachStdout: true,
		AttachStderr: true,
	})
	if err != nil {
		return err
	}

	attach, err := m.cli.ContainerExecAttach(opCtx, execResp.ID, types.ExecStartCheck{})
	if err != nil {
		return err
	}
	defer attach.Close()

	go func() {
		_, _ = io.Copy(io.Discard, attach.Reader)
	}()

	return m.waitForExecExit(opCtx, execResp.ID)
}

func (m *PoolManager) waitForExecExit(ctx context.Context, execID string) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		inspect, err := m.cli.ContainerExecInspect(ctx, execID)
		if err != nil {
			return err
		}
		if !inspect.Running {
			if inspect.ExitCode != 0 {
				return fmt.Errorf("exec %s failed with exit code %d", execID, inspect.ExitCode)
			}
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (m *PoolManager) waitForContainerExit(ctx context.Context, containerID string) error {
	ticker := time.NewTicker(150 * time.Millisecond)
	defer ticker.Stop()

	for {
		inspect, err := m.cli.ContainerInspect(ctx, containerID)
		if err != nil {
			return err
		}
		if inspect.State != nil && !inspect.State.Running {
			if inspect.State.ExitCode != 0 {
				return fmt.Errorf("container %s exited with code %d", containerID, inspect.State.ExitCode)
			}
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (m *PoolManager) ensureImage(ctx context.Context) error {
	opCtx, cancel := context.WithTimeout(ctx, m.cfg.DockerTimeout)
	defer cancel()

	if _, _, err := m.cli.ImageInspectWithRaw(opCtx, m.cfg.ImageName); err != nil {
		return fmt.Errorf("image %q not found; build it first with `docker build -t %s .`: %w", m.cfg.ImageName, m.cfg.ImageName, err)
	}
	return nil
}

func (m *PoolManager) reconcileWarmPool(ctx context.Context) error {
	if err := m.ensureTier1(ctx); err != nil {
		return err
	}
	return m.ensureTier2(ctx)
}

func (m *PoolManager) ensureTier1(ctx context.Context) error {
	m.mu.Lock()
	if m.tier1 != nil || m.tier1Provisioning {
		m.mu.Unlock()
		return nil
	}
	m.tier1Provisioning = true
	m.mu.Unlock()

	defer func() {
		m.mu.Lock()
		m.tier1Provisioning = false
		m.mu.Unlock()
	}()

	ctr, err := m.createResidentContainer(ctx, 1, false)
	if err != nil {
		return err
	}

	m.mu.Lock()
	if m.tier1 == nil {
		m.tier1 = ctr
	} else {
		go m.destroyManagedContainer(context.Background(), ctr)
	}
	m.mu.Unlock()

	return nil
}

func (m *PoolManager) ensureTier2(ctx context.Context) error {
	m.mu.Lock()
	if m.tier2 != nil || m.tier2Provisioning {
		m.mu.Unlock()
		return nil
	}
	m.tier2Provisioning = true
	m.mu.Unlock()

	defer func() {
		m.mu.Lock()
		m.tier2Provisioning = false
		m.mu.Unlock()
	}()

	ctr, err := m.createResidentContainer(ctx, 2, true)
	if err != nil {
		return err
	}

	m.mu.Lock()
	if m.tier2 == nil {
		m.tier2 = ctr
	} else {
		go m.destroyManagedContainer(context.Background(), ctr)
	}
	m.mu.Unlock()

	return nil
}

func (m *PoolManager) createResidentContainer(ctx context.Context, tier int, pause bool) (*managedContainer, error) {
	opCtx, cancel := context.WithTimeout(ctx, m.cfg.DockerTimeout)
	defer cancel()

	resp, err := m.cli.ContainerCreate(
		opCtx,
		&container.Config{
			Image: m.cfg.ImageName,
			Cmd:   []string{"sleep", "infinity"},
			Tty:   false,
			Labels: map[string]string{
				"simulator-tier": strconv.Itoa(tier),
			},
		},
		nil,
		nil,
		nil,
		fmt.Sprintf("faas-tier%d-%d", tier, time.Now().UnixNano()),
	)
	if err != nil {
		return nil, err
	}

	if err := m.cli.ContainerStart(opCtx, resp.ID, types.ContainerStartOptions{}); err != nil {
		_ = m.cli.ContainerRemove(context.Background(), resp.ID, types.ContainerRemoveOptions{Force: true})
		return nil, err
	}

	if pause {
		if err := m.cli.ContainerPause(opCtx, resp.ID); err != nil {
			_ = m.cli.ContainerRemove(context.Background(), resp.ID, types.ContainerRemoveOptions{Force: true})
			return nil, err
		}
	}

	return &managedContainer{
		ID:       resp.ID,
		Name:     fmt.Sprintf("tier-%d", tier),
		Tier:     tier,
		LastUsed: time.Now(),
		Paused:   pause,
	}, nil
}

func (m *PoolManager) evictIdle(ctx context.Context) {
	var evictList []*managedContainer

	m.mu.Lock()
	now := time.Now()
	if m.tier2 != nil && !m.tier2.Busy && now.Sub(m.tier2.LastUsed) >= m.cfg.EvictionAfter {
		evictList = append(evictList, m.tier2)
		m.tier2 = nil
	}
	if m.tier1 != nil && !m.tier1.Busy && now.Sub(m.tier1.LastUsed) >= m.cfg.EvictionAfter {
		evictList = append(evictList, m.tier1)
		m.tier1 = nil
	}
	m.mu.Unlock()

	for _, ctr := range evictList {
		opCtx, cancel := context.WithTimeout(ctx, m.cfg.DockerTimeout)
		if err := m.destroyManagedContainer(opCtx, ctr); err != nil {
			log.Printf("evict %s: %v", ctr.Name, err)
		} else {
			log.Printf("evicted idle tier %d container %s", ctr.Tier, ctr.ID)
		}
		cancel()
	}
}

func (m *PoolManager) destroyManagedContainer(ctx context.Context, ctr *managedContainer) error {
	if ctr == nil {
		return nil
	}

	if ctr.Paused {
		if err := m.cli.ContainerUnpause(ctx, ctr.ID); err != nil {
			log.Printf("unpause before remove %s: %v", ctr.ID, err)
		}
	}

	timeout := 2
	if err := m.cli.ContainerStop(ctx, ctr.ID, container.StopOptions{Timeout: &timeout}); err != nil && !client.IsErrNotFound(err) {
		log.Printf("stop container %s: %v", ctr.ID, err)
	}

	if err := m.cli.ContainerRemove(ctx, ctr.ID, types.ContainerRemoveOptions{Force: true}); err != nil && !client.IsErrNotFound(err) {
		return err
	}

	return nil
}

func (m *PoolManager) invalidateTier(tier int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	switch tier {
	case 1:
		m.tier1 = nil
	case 2:
		m.tier2 = nil
	}
}

func (m *PoolManager) markTierUsed(tier int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	switch tier {
	case 1:
		if m.tier1 != nil {
			m.tier1.LastUsed = now
		}
	case 2:
		if m.tier2 != nil {
			m.tier2.LastUsed = now
			m.tier2.Paused = true
		}
	}
}

func (m *PoolManager) nextRequestID() string {
	seq := m.requestSeq.Add(1)
	return fmt.Sprintf("req-%d-%d", time.Now().UnixNano(), seq)
}
