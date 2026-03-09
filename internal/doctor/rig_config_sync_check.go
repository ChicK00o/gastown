package doctor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/steveyegge/gastown/internal/config"
	"github.com/steveyegge/gastown/internal/rig"
)

// RigConfigSyncCheck verifies that all registered rigs have a config.json file.
// This prevents issues where the daemon can't find the beads prefix to check
// docked/parked status, causing it to auto-start agents for docked rigs.
type RigConfigSyncCheck struct {
	FixableCheck
	missingConfig   []string          // Rig names missing config.json
	prefixMismatches []prefixMismatch // Prefix mismatches between config.json and registry
}

type prefixMismatch struct {
	rigName       string
	configPrefix  string
	registryPrefix string
}

// NewRigConfigSyncCheck creates a new rig config sync check.
func NewRigConfigSyncCheck() *RigConfigSyncCheck {
	return &RigConfigSyncCheck{
		FixableCheck: FixableCheck{
			BaseCheck: BaseCheck{
				CheckName:        "rig-config-sync",
				CheckDescription: "Verify registered rigs have config.json with correct prefix",
				CheckCategory:    CategoryConfig,
			},
		},
	}
}

// Run checks if all registered rigs have config.json files.
func (c *RigConfigSyncCheck) Run(ctx *CheckContext) *CheckResult {
	rigsConfigPath := filepath.Join(ctx.TownRoot, "mayor", "rigs.json")
	rigsConfig, err := config.LoadRigsConfig(rigsConfigPath)
	if err != nil {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusWarning,
			Message: "Could not load rigs registry",
			Details: []string{err.Error()},
		}
	}

	c.missingConfig = nil
	c.prefixMismatches = nil
	var details []string

	for rigName, entry := range rigsConfig.Rigs {
		rigPath := filepath.Join(ctx.TownRoot, rigName)
		configPath := filepath.Join(rigPath, "config.json")

		// Check if rig directory exists
		if _, err := os.Stat(rigPath); os.IsNotExist(err) {
			details = append(details, fmt.Sprintf("Registered rig %s directory does not exist", rigName))
			continue
		}

		// Check if config.json exists
		if _, err := os.Stat(configPath); os.IsNotExist(err) {
			c.missingConfig = append(c.missingConfig, rigName)
			details = append(details, fmt.Sprintf("Rig %s is registered but missing config.json", rigName))
			continue
		}

		// Check if config.json has correct prefix
		rigCfg, err := rig.LoadRigConfig(rigPath)
		if err != nil {
			details = append(details, fmt.Sprintf("Rig %s has unreadable config.json: %v", rigName, err))
			continue
		}

		// Compare prefixes
		registryPrefix := ""
		if entry.BeadsConfig != nil {
			registryPrefix = entry.BeadsConfig.Prefix
		}
		configPrefix := ""
		if rigCfg.Beads != nil {
			configPrefix = rigCfg.Beads.Prefix
		}

		if registryPrefix != "" && configPrefix != "" && registryPrefix != configPrefix {
			c.prefixMismatches = append(c.prefixMismatches, prefixMismatch{
				rigName:        rigName,
				configPrefix:   configPrefix,
				registryPrefix: registryPrefix,
			})
			details = append(details, fmt.Sprintf(
				"Rig %s prefix mismatch: config.json has %q, registry has %q",
				rigName, configPrefix, registryPrefix))
		}
	}

	if len(c.missingConfig) == 0 && len(c.prefixMismatches) == 0 {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusOK,
			Message: "All registered rigs have valid config.json",
		}
	}

	msg := ""
	if len(c.missingConfig) > 0 {
		msg = fmt.Sprintf("%d rig(s) missing config.json", len(c.missingConfig))
	}
	if len(c.prefixMismatches) > 0 {
		if msg != "" {
			msg += ", "
		}
		msg += fmt.Sprintf("%d prefix mismatch(es)", len(c.prefixMismatches))
	}

	return &CheckResult{
		Name:    c.Name(),
		Status:  StatusWarning,
		Message: msg,
		Details: details,
		FixHint: "Run 'gt doctor --fix' to create missing config.json files from registry",
	}
}

// Fix creates missing config.json files from the registry.
func (c *RigConfigSyncCheck) Fix(ctx *CheckContext) error {
	rigsConfigPath := filepath.Join(ctx.TownRoot, "mayor", "rigs.json")
	rigsConfig, err := config.LoadRigsConfig(rigsConfigPath)
	if err != nil {
		return fmt.Errorf("could not load rigs registry: %w", err)
	}

	for _, rigName := range c.missingConfig {
		entry, ok := rigsConfig.Rigs[rigName]
		if !ok {
			continue
		}

		rigPath := filepath.Join(ctx.TownRoot, rigName)
		configPath := filepath.Join(rigPath, "config.json")

		// Get prefix from registry
		prefix := ""
		if entry.BeadsConfig != nil {
			prefix = entry.BeadsConfig.Prefix
		}

		// Create config.json
		rigCfg := &rig.RigConfig{
			Type:      "rig",
			Version:   1,
			Name:      rigName,
			GitURL:    entry.GitURL,
			CreatedAt: entry.AddedAt,
		}
		if prefix != "" {
			rigCfg.Beads = &rig.BeadsConfig{Prefix: prefix}
		}

		data, err := json.MarshalIndent(rigCfg, "", "  ")
		if err != nil {
			return fmt.Errorf("could not serialize config for %s: %w", rigName, err)
		}

		if err := os.WriteFile(configPath, data, 0644); err != nil {
			return fmt.Errorf("could not write config.json for %s: %w", rigName, err)
		}
	}

	return nil
}