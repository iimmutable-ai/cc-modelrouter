// Package daemon manages router instances.
package daemon

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/iimmutable-ai/cc-modelrouter/internal/config"
)

// InstanceMetadata represents metadata for a running instance.
type InstanceMetadata struct {
	ID           string    `json:"id"`
	Port         int       `json:"port"`
	PID          int       `json:"pid"`
	ConfigType   string    `json:"configType"`
	ConfigPath   string    `json:"configPath"`
	ProjectRoot  string    `json:"projectRoot"`
	StartTime    time.Time `json:"startTime"`
	ActiveProfile string   `json:"activeProfile,omitempty"` // Current active profile name
	AdminToken    string   `json:"adminToken,omitempty"`    // Admin API token for runtime management
}

// GenerateInstanceID generates a unique instance ID.
func GenerateInstanceID() string {
	return fmt.Sprintf("inst_%s", time.Now().Format("20060102_150405"))
}

// InstancesDir returns the directory for instance files.
func InstancesDir() (string, error) {
	dataDir, err := config.DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dataDir, "instances"), nil
}

// SaveInstance saves instance metadata to disk.
func SaveInstance(meta *InstanceMetadata) error {
	dir, err := InstancesDir()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	path := filepath.Join(dir, meta.ID+".json")
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0600)
}

// LoadInstance loads instance metadata from disk.
func LoadInstance(id string) (*InstanceMetadata, error) {
	dir, err := InstancesDir()
	if err != nil {
		return nil, err
	}

	path := filepath.Join(dir, id+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var meta InstanceMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, err
	}

	return &meta, nil
}

// DeleteInstance removes instance metadata from disk.
func DeleteInstance(id string) error {
	dir, err := InstancesDir()
	if err != nil {
		return err
	}

	path := filepath.Join(dir, id+".json")
	return os.Remove(path)
}

// ListInstances lists all instances.
func ListInstances() ([]*InstanceMetadata, error) {
	dir, err := InstancesDir()
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var instances []*InstanceMetadata
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		id := strings.TrimSuffix(entry.Name(), ".json")
		meta, err := LoadInstance(id)
		if err != nil {
			continue
		}

		instances = append(instances, meta)
	}

	return instances, nil
}

// IsRunning checks if an instance is still running.
func IsRunning(meta *InstanceMetadata) bool {
	if meta.PID == 0 {
		return false
	}

	proc, err := os.FindProcess(meta.PID)
	if err != nil {
		return false
	}

	// On Unix, FindProcess always succeeds, so we need to signal
	err = proc.Signal(syscall.Signal(0))
	return err == nil
}

// UpdateActiveProfile updates the active profile for an instance.
func UpdateActiveProfile(id string, profile string) error {
	meta, err := LoadInstance(id)
	if err != nil {
		return err
	}
	meta.ActiveProfile = profile
	return SaveInstance(meta)
}

// GenerateAdminToken generates a random admin token for secure API access.
func GenerateAdminToken() string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	const length = 32

	b := make([]byte, length)
	for i := range b {
		b[i] = charset[rand.Intn(len(charset))]
	}
	return string(b)
}
