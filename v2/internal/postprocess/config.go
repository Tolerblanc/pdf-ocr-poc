package postprocess

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

const postprocessConfigEnv = "OCRPOC_POSTPROCESS_CONFIG"

type ResolvedConfig struct {
	Path        string
	Profile     string
	AllowRemote *bool
	Config      Config
}

func ResolveConfig(requestedProvider string, configPath string) (ResolvedConfig, error) {
	configPath = firstNonEmpty(strings.TrimSpace(configPath), strings.TrimSpace(os.Getenv(postprocessConfigEnv)))
	requestedProvider = strings.TrimSpace(requestedProvider)
	if configPath == "" {
		config := Config{Provider: NormalizeProviderName(requestedProvider)}
		if err := validateResolvedConfig(config); err != nil {
			return ResolvedConfig{}, err
		}
		return ResolvedConfig{
			Config: config,
		}, nil
	}

	body, err := os.ReadFile(configPath)
	if err != nil {
		return ResolvedConfig{}, fmt.Errorf("read postprocess config: %w", err)
	}

	file := ConfigFile{}
	if err := json.Unmarshal(body, &file); err != nil {
		return ResolvedConfig{}, fmt.Errorf("parse postprocess config: %w", err)
	}
	if strings.TrimSpace(file.Version) != SchemaVersion {
		return ResolvedConfig{}, fmt.Errorf("unsupported postprocess config version: %s", strings.TrimSpace(file.Version))
	}
	if err := validateProfileKeys(file.Providers); err != nil {
		return ResolvedConfig{}, err
	}

	resolved, profile, err := resolveConfigFromFile(file, requestedProvider)
	if err != nil {
		return ResolvedConfig{}, err
	}
	if err := validateResolvedConfig(resolved); err != nil {
		return ResolvedConfig{}, err
	}

	return ResolvedConfig{
		Path:        configPath,
		Profile:     profile,
		AllowRemote: file.Runtime.AllowRemote,
		Config:      resolved,
	}, nil
}

func validateResolvedConfig(config Config) error {
	if !isSupportedProviderName(config.Provider) {
		return fmt.Errorf("unsupported postprocess provider: %s", config.Provider)
	}
	outputMode := strings.TrimSpace(config.OutputMode)
	if outputMode != "" && outputMode != OutputModeSidecarOnly && outputMode != OutputModePrimaryArtifacts {
		return fmt.Errorf("unsupported postprocess output_mode: %s", outputMode)
	}
	return nil
}

func ValidateExecution(resolved ResolvedConfig, localOnly bool) error {
	if resolved.AllowRemote != nil && !*resolved.AllowRemote && providerRequiresRemote(resolved.Config.Provider) {
		return fmt.Errorf("postprocess config forbids remote providers: %s", resolved.Config.Provider)
	}
	if localOnly && providerRequiresRemote(resolved.Config.Provider) {
		return fmt.Errorf("postprocess provider %s is not allowed when local-only mode is enabled", resolved.Config.Provider)
	}
	return nil
}

func validateProfileKeys(providers map[string]Config) error {
	for name := range providers {
		if isSupportedProviderName(name) {
			return fmt.Errorf("reserved postprocess profile name: %s", name)
		}
	}
	return nil
}

func resolveConfigFromFile(file ConfigFile, requestedProvider string) (Config, string, error) {
	requestedProvider = strings.TrimSpace(requestedProvider)
	selected := Config{}
	selectedProfile := ""
	requestedLiteralProvider := requestedProvider != "" && isSupportedProviderName(requestedProvider)

	if requestedProvider != "" && !requestedLiteralProvider {
		if profileConfig, ok := file.Providers[requestedProvider]; ok {
			selected = profileConfig
			selectedProfile = requestedProvider
		}
	}

	if selectedProfile == "" && strings.TrimSpace(file.Runtime.Profile) != "" {
		profileName := strings.TrimSpace(file.Runtime.Profile)
		profileConfig, ok := file.Providers[profileName]
		if !ok {
			return Config{}, "", fmt.Errorf("unknown postprocess runtime profile: %s", profileName)
		}
		selected = profileConfig
		selectedProfile = profileName
	}

	if selectedProfile == "" {
		switch len(file.Providers) {
		case 0:
			// No named profiles; runtime/provider overrides still apply below.
		case 1:
			profileNames := sortedConfigKeys(file.Providers)
			selectedProfile = profileNames[0]
			selected = file.Providers[selectedProfile]
		default:
			if requestedProvider == "" && strings.TrimSpace(file.Runtime.Provider) == "" {
				return Config{}, "", fmt.Errorf(
					"postprocess config is ambiguous; set runtime.profile or pass --postprocess-provider",
				)
			}
		}
	}

	if strings.TrimSpace(selected.AuthRef) != "" && selected.Auth == nil {
		auth, ok := file.Credentials[strings.TrimSpace(selected.AuthRef)]
		if !ok {
			return Config{}, "", fmt.Errorf("unknown postprocess auth_ref: %s", selected.AuthRef)
		}
		selected.Auth = &auth
	}

	providerOverride := ""
	if requestedProvider != "" {
		if _, ok := file.Providers[requestedProvider]; !ok {
			providerOverride = requestedProvider
		} else if requestedLiteralProvider {
			providerOverride = requestedProvider
		}
	} else if strings.TrimSpace(file.Runtime.Override.Provider) != "" {
		providerOverride = strings.TrimSpace(file.Runtime.Override.Provider)
	} else if strings.TrimSpace(file.Runtime.Provider) != "" {
		providerOverride = strings.TrimSpace(file.Runtime.Provider)
	}

	applyRuntimeOverride(&selected, file.Runtime.Override)
	if providerOverride != "" {
		selected.Provider = providerOverride
	}
	if strings.TrimSpace(selected.Provider) == "" {
		selected.Provider = NormalizeProviderName(requestedProvider)
	}
	if strings.TrimSpace(selected.Provider) == "" {
		selected.Provider = ProviderNone
	}
	selected.Provider = NormalizeProviderName(selected.Provider)

	return selected, selectedProfile, nil
}

func applyRuntimeOverride(config *Config, override RuntimeOverride) {
	if config == nil {
		return
	}
	if strings.TrimSpace(override.Provider) != "" {
		config.Provider = strings.TrimSpace(override.Provider)
	}
	if strings.TrimSpace(override.Model) != "" {
		config.Model = override.Model
	}
	if strings.TrimSpace(override.BaseURL) != "" {
		config.BaseURL = override.BaseURL
	}
	if strings.TrimSpace(override.IssuerURL) != "" {
		config.IssuerURL = override.IssuerURL
	}
	if override.TimeoutSeconds > 0 {
		config.TimeoutSeconds = override.TimeoutSeconds
	}
	if override.Temperature != nil {
		value := *override.Temperature
		config.Temperature = &value
	}
	if override.MaxCompletionTokens > 0 {
		config.MaxCompletionTokens = override.MaxCompletionTokens
	}
	if strings.TrimSpace(override.OutputMode) != "" {
		config.OutputMode = strings.TrimSpace(override.OutputMode)
	}
}

func sortedConfigKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func isSupportedProviderName(value string) bool {
	value = strings.TrimSpace(value)
	for _, candidate := range SupportedProviders() {
		if value == candidate {
			return true
		}
	}
	return false
}

func providerRequiresRemote(value string) bool {
	switch NormalizeProviderName(value) {
	case ProviderCloudLLM, ProviderCodexHeadlessOAuth:
		return true
	default:
		return false
	}
}
