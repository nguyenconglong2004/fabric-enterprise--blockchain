package raft

import (
	"sync"
	"time"

	netpkg "raft-order-service/internal/network"
)

// Config holds runtime-tunable parameters for a RaftNode.
// All fields are protected by a read-write mutex; use the typed getters/setters for thread-safe access.
type Config struct {
	mu sync.RWMutex

	HeartbeatInterval    time.Duration
	HeartbeatTimeout     time.Duration
	DetectionTimeout     time.Duration
	AutoProposeInterval  time.Duration
	AutoProposeBlockSize int
	SyncDiscoveryWindow  time.Duration
	SyncFetchTimeout     time.Duration
	SyncShardSize        int
}

// DefaultConfig returns a Config populated with the default constants from protocol.go.
func DefaultConfig() *Config {
	return &Config{
		HeartbeatInterval:    netpkg.HeartbeatInterval,
		HeartbeatTimeout:     netpkg.HeartbeatTimeout,
		DetectionTimeout:     netpkg.DetectionTimeout,
		AutoProposeInterval:  AutoProposeInterval,
		AutoProposeBlockSize: AutoProposeBlockSize,
		SyncDiscoveryWindow:  netpkg.SyncDiscoveryWindow,
		SyncFetchTimeout:     netpkg.SyncFetchTimeout,
		SyncShardSize:        netpkg.SyncShardSize,
	}
}

func (c *Config) GetHeartbeatInterval() time.Duration {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.HeartbeatInterval
}

func (c *Config) SetHeartbeatInterval(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.HeartbeatInterval = d
}

func (c *Config) GetHeartbeatTimeout() time.Duration {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.HeartbeatTimeout
}

func (c *Config) SetHeartbeatTimeout(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.HeartbeatTimeout = d
}

func (c *Config) GetDetectionTimeout() time.Duration {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.DetectionTimeout
}

func (c *Config) SetDetectionTimeout(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.DetectionTimeout = d
}

func (c *Config) GetAutoProposalInterval() time.Duration {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.AutoProposeInterval
}

func (c *Config) SetAutoProposalInterval(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.AutoProposeInterval = d
}

func (c *Config) GetAutoProposalBlockSize() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.AutoProposeBlockSize
}

func (c *Config) SetAutoProposalBlockSize(n int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.AutoProposeBlockSize = n
}

func (c *Config) GetSyncDiscoveryWindow() time.Duration {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.SyncDiscoveryWindow
}

func (c *Config) SetSyncDiscoveryWindow(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.SyncDiscoveryWindow = d
}

func (c *Config) GetSyncFetchTimeout() time.Duration {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.SyncFetchTimeout
}

func (c *Config) SetSyncFetchTimeout(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.SyncFetchTimeout = d
}

func (c *Config) GetSyncShardSize() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.SyncShardSize
}

func (c *Config) SetSyncShardSize(n int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.SyncShardSize = n
}

// ConfigSnapshot is a JSON-serializable snapshot of all config values.
type ConfigSnapshot struct {
	HeartbeatIntervalMs   int64 `json:"heartbeat_interval_ms"`
	HeartbeatTimeoutMs    int64 `json:"heartbeat_timeout_ms"`
	DetectionTimeoutMs    int64 `json:"detection_timeout_ms"`
	AutoProposeIntervalMs int64 `json:"auto_propose_interval_ms"`
	AutoProposeBlockSize  int   `json:"auto_propose_block_size"`
	SyncDiscoveryWindowMs int64 `json:"sync_discovery_window_ms"`
	SyncFetchTimeoutMs    int64 `json:"sync_fetch_timeout_ms"`
	SyncShardSize         int   `json:"sync_shard_size"`
}

// ConfigJSON is a JSON-patchable representation of Config for REST PATCH and POST bodies.
// All duration fields are in milliseconds. Zero values are ignored (not applied).
type ConfigJSON struct {
	HeartbeatIntervalMs   *int64 `json:"heartbeat_interval_ms,omitempty"`
	HeartbeatTimeoutMs    *int64 `json:"heartbeat_timeout_ms,omitempty"`
	DetectionTimeoutMs    *int64 `json:"detection_timeout_ms,omitempty"`
	AutoProposeIntervalMs *int64 `json:"auto_propose_interval_ms,omitempty"`
	AutoProposeBlockSize  *int   `json:"auto_propose_block_size,omitempty"`
	SyncDiscoveryWindowMs *int64 `json:"sync_discovery_window_ms,omitempty"`
	SyncFetchTimeoutMs    *int64 `json:"sync_fetch_timeout_ms,omitempty"`
	SyncShardSize         *int   `json:"sync_shard_size,omitempty"`
}

// ApplyTo applies all non-nil fields of j to cfg.
func (j *ConfigJSON) ApplyTo(cfg *Config) {
	if j.HeartbeatIntervalMs != nil {
		cfg.SetHeartbeatInterval(time.Duration(*j.HeartbeatIntervalMs) * time.Millisecond)
	}
	if j.HeartbeatTimeoutMs != nil {
		cfg.SetHeartbeatTimeout(time.Duration(*j.HeartbeatTimeoutMs) * time.Millisecond)
	}
	if j.DetectionTimeoutMs != nil {
		cfg.SetDetectionTimeout(time.Duration(*j.DetectionTimeoutMs) * time.Millisecond)
	}
	if j.AutoProposeIntervalMs != nil {
		cfg.SetAutoProposalInterval(time.Duration(*j.AutoProposeIntervalMs) * time.Millisecond)
	}
	if j.AutoProposeBlockSize != nil {
		cfg.SetAutoProposalBlockSize(*j.AutoProposeBlockSize)
	}
	if j.SyncDiscoveryWindowMs != nil {
		cfg.SetSyncDiscoveryWindow(time.Duration(*j.SyncDiscoveryWindowMs) * time.Millisecond)
	}
	if j.SyncFetchTimeoutMs != nil {
		cfg.SetSyncFetchTimeout(time.Duration(*j.SyncFetchTimeoutMs) * time.Millisecond)
	}
	if j.SyncShardSize != nil {
		cfg.SetSyncShardSize(*j.SyncShardSize)
	}
}

// Snapshot returns a copy of all config values for JSON serialization.
func (c *Config) Snapshot() ConfigSnapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return ConfigSnapshot{
		HeartbeatIntervalMs:   c.HeartbeatInterval.Milliseconds(),
		HeartbeatTimeoutMs:    c.HeartbeatTimeout.Milliseconds(),
		DetectionTimeoutMs:    c.DetectionTimeout.Milliseconds(),
		AutoProposeIntervalMs: c.AutoProposeInterval.Milliseconds(),
		AutoProposeBlockSize:  c.AutoProposeBlockSize,
		SyncDiscoveryWindowMs: c.SyncDiscoveryWindow.Milliseconds(),
		SyncFetchTimeoutMs:    c.SyncFetchTimeout.Milliseconds(),
		SyncShardSize:         c.SyncShardSize,
	}
}
