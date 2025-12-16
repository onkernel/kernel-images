package policy

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

const PolicyPath = "/etc/chromium/policies/managed/policy.json"

// Policy represents the Chrome enterprise policy structure
type Policy struct {
	mu sync.Mutex

	PasswordManagerEnabled      bool                        `json:"PasswordManagerEnabled"`
	AutofillCreditCardEnabled   bool                        `json:"AutofillCreditCardEnabled"`
	TranslateEnabled            bool                        `json:"TranslateEnabled"`
	DefaultNotificationsSetting int                         `json:"DefaultNotificationsSetting"`
	ExtensionSettings           map[string]ExtensionSetting `json:"ExtensionSettings"`
}

// ExtensionSetting represents settings for a specific extension
type ExtensionSetting struct {
	InstallationMode    string   `json:"installation_mode,omitempty"`
	Path                string   `json:"path,omitempty"`
	AllowedTypes        []string `json:"allowed_types,omitempty"`
	InstallSources      []string `json:"install_sources,omitempty"`
	RuntimeBlockedHosts []string `json:"runtime_blocked_hosts,omitempty"`
	RuntimeAllowedHosts []string `json:"runtime_allowed_hosts,omitempty"`
}

// readPolicyUnlocked reads the current enterprise policy from disk without locking
// This is an internal helper for use within already-locked operations
func (p *Policy) readPolicyUnlocked() (*Policy, error) {
	data, err := os.ReadFile(PolicyPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Return default policy if file doesn't exist
			return &Policy{
				PasswordManagerEnabled:      false,
				AutofillCreditCardEnabled:   false,
				TranslateEnabled:            false,
				DefaultNotificationsSetting: 2,
				ExtensionSettings:           make(map[string]ExtensionSetting),
			}, nil
		}
		return nil, fmt.Errorf("failed to read policy file: %w", err)
	}

	var policy Policy
	if err := json.Unmarshal(data, &policy); err != nil {
		return nil, fmt.Errorf("failed to parse policy file: %w", err)
	}

	// Initialize ExtensionSettings map if it's nil to prevent panic on write
	if policy.ExtensionSettings == nil {
		policy.ExtensionSettings = make(map[string]ExtensionSetting)
	}

	return &policy, nil
}

// ReadPolicy reads the current enterprise policy from disk
func (p *Policy) ReadPolicy() (*Policy, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.readPolicyUnlocked()
}

// writePolicyUnlocked writes the policy to disk without locking
// This is an internal helper for use within already-locked operations
func (p *Policy) writePolicyUnlocked(policy *Policy) error {
	data, err := json.MarshalIndent(policy, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal policy: %w", err)
	}

	if err := os.WriteFile(PolicyPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write policy file: %w", err)
	}

	return nil
}

// WritePolicy writes the policy to disk
func (p *Policy) WritePolicy(policy *Policy) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.writePolicyUnlocked(policy)
}

// AddExtension adds or updates an extension in the policy
// extensionID should be a stable identifier (can be derived from extension path)
func (p *Policy) AddExtension(extensionID, extensionPath string, requiresEnterprisePolicy bool) error {
	// Lock for the entire read-modify-write cycle to prevent race conditions
	p.mu.Lock()
	defer p.mu.Unlock()

	policy, err := p.readPolicyUnlocked()
	if err != nil {
		return err
	}

	// Ensure the wildcard policy exists
	if _, exists := policy.ExtensionSettings["*"]; !exists {
		policy.ExtensionSettings["*"] = ExtensionSetting{
			AllowedTypes:   []string{"extension"},
			InstallSources: []string{"*"},
		}
	}

	// Add the specific extension
	setting := ExtensionSetting{
		Path: extensionPath,
	}

	// If the extension requires enterprise policy (like webRequestBlocking),
	// set it as force_installed https://github.com/cloudflare/web-bot-auth/blob/main/examples/browser-extension/policy/policy.json.templ
	if requiresEnterprisePolicy {
		setting.InstallationMode = "force_installed"
		// Allow all hosts for webRequest APIs
		setting.RuntimeAllowedHosts = []string{"*://*/*"}
	} else {
		setting.InstallationMode = "normal_installed"
	}

	policy.ExtensionSettings[extensionID] = setting

	return p.writePolicyUnlocked(policy)
}

// GenerateExtensionID returns a stable identifier for the extension policy.
// For ExtensionSettings with local paths, Chrome allows custom identifiers.
// We use the extension name because it's stable, readable, and matches the directory.
func (p *Policy) GenerateExtensionID(extensionName string) string {
	return extensionName
}

// RequiresEnterprisePolicy checks if an extension requires enterprise policy
// by examining its manifest.json for webRequestBlocking or webRequest permissions
func (p *Policy) RequiresEnterprisePolicy(manifestPath string) (bool, error) {
	manifestData, err := os.ReadFile(manifestPath)
	if err != nil {
		return false, err
	}

	var manifest map[string]interface{}
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		return false, err
	}

	// Check if permissions include webRequestBlocking or webRequest
	perms, ok := manifest["permissions"].([]interface{})
	if !ok {
		return false, nil
	}

	for _, perm := range perms {
		if permStr, ok := perm.(string); ok {
			if permStr == "webRequestBlocking" || permStr == "webRequest" {
				return true, nil
			}
		}
	}

	return false, nil
}
