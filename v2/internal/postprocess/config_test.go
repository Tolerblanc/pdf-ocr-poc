package postprocess

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveConfigUsesEnvPathAndRuntimeProfile(t *testing.T) {
	temp := t.TempDir()
	configPath := filepath.Join(temp, "postprocess.json")
	body := `{
	  "version": "v1alpha1",
	  "credentials": {
	    "openai": {
	      "kind": "oauth_store_file",
	      "file": "/tmp/auth.json",
	      "provider_id": "openai"
	    }
	  },
	  "providers": {
	    "default": {
	      "provider": "codex-headless-oauth",
	      "auth_ref": "openai",
	      "model": "gpt-test",
	      "output_mode": "sidecar_only"
	    }
	  },
	  "runtime": {
	    "profile": "default",
	    "override": {
	      "output_mode": "primary_artifacts",
	      "timeout_seconds": 90
	    }
	  }
	}`
	if err := os.WriteFile(configPath, []byte(body), 0o644); err != nil {
		t.Fatalf("write config failed: %v", err)
	}
	t.Setenv(postprocessConfigEnv, configPath)

	resolved, err := ResolveConfig("", "")
	if err != nil {
		t.Fatalf("resolve config failed: %v", err)
	}
	if resolved.Path != configPath {
		t.Fatalf("expected config path %s, got %s", configPath, resolved.Path)
	}
	if resolved.Profile != "default" {
		t.Fatalf("expected profile default, got %s", resolved.Profile)
	}
	if resolved.Config.Provider != ProviderCodexHeadlessOAuth {
		t.Fatalf("expected provider %s, got %s", ProviderCodexHeadlessOAuth, resolved.Config.Provider)
	}
	if resolved.Config.Auth == nil || resolved.Config.Auth.Kind != AuthKindOAuthStoreFile {
		t.Fatalf("expected resolved auth_ref, got %+v", resolved.Config.Auth)
	}
	if resolved.Config.OutputMode != OutputModePrimaryArtifacts {
		t.Fatalf("expected primary output mode, got %s", resolved.Config.OutputMode)
	}
	if resolved.Config.TimeoutSeconds != 90 {
		t.Fatalf("expected timeout override, got %d", resolved.Config.TimeoutSeconds)
	}
}

func TestResolveConfigAllowsExplicitProviderOverride(t *testing.T) {
	file := ConfigFile{
		Version: SchemaVersion,
		Providers: map[string]Config{
			"default": {
				Provider: ProviderCodexHeadlessOAuth,
				Model:    "gpt-test",
			},
		},
		Runtime: RuntimeSelection{Profile: "default"},
	}

	resolved, profile, err := resolveConfigFromFile(file, ProviderNone)
	if err != nil {
		t.Fatalf("resolve config failed: %v", err)
	}
	if profile != "default" {
		t.Fatalf("expected default profile, got %s", profile)
	}
	if resolved.Provider != ProviderNone {
		t.Fatalf("expected provider override to none, got %s", resolved.Provider)
	}
	if resolved.Model != "gpt-test" {
		t.Fatalf("expected profile values to be preserved, got %+v", resolved)
	}
}

func TestResolveConfigRejectsAmbiguousProfiles(t *testing.T) {
	_, _, err := resolveConfigFromFile(ConfigFile{
		Version: SchemaVersion,
		Providers: map[string]Config{
			"a": {Provider: ProviderNone},
			"b": {Provider: ProviderCodexHeadlessOAuth},
		},
	}, "")
	if err == nil {
		t.Fatalf("expected ambiguous profile error")
	}
}

func TestResolveConfigPreservesExplicitZeroTemperatureOverride(t *testing.T) {
	zero := 0.0
	defaultTemperature := 0.7
	resolved, _, err := resolveConfigFromFile(ConfigFile{
		Version: SchemaVersion,
		Providers: map[string]Config{
			"default": {
				Provider:    ProviderCodexHeadlessOAuth,
				Temperature: &defaultTemperature,
			},
		},
		Runtime: RuntimeSelection{
			Profile: "default",
			Override: RuntimeOverride{
				Temperature: &zero,
			},
		},
	}, "")
	if err != nil {
		t.Fatalf("resolve config failed: %v", err)
	}
	if resolved.Temperature == nil || *resolved.Temperature != 0 {
		t.Fatalf("expected zero temperature override, got %v", resolved.Temperature)
	}
}

func TestValidateExecutionAllowsRemoteWhenExplicitlyEnabled(t *testing.T) {
	err := ValidateExecution(ResolvedConfig{Config: Config{Provider: ProviderCodexHeadlessOAuth}}, true)
	if err != nil {
		t.Fatalf("expected remote postprocess to be allowed, got %v", err)
	}
}

func TestValidateExecutionRejectsRemoteWhenCLIFlagDisallowsIt(t *testing.T) {
	err := ValidateExecution(ResolvedConfig{Config: Config{Provider: ProviderCodexHeadlessOAuth}}, false)
	if err == nil {
		t.Fatalf("expected remote postprocess rejection")
	}
	if got := err.Error(); got == "" || !strings.Contains(got, "--postprocess-allow-remote") {
		t.Fatalf("expected flag guidance, got %v", err)
	}
}

func TestValidateExecutionRejectsRemoteWhenConfigDisallowsIt(t *testing.T) {
	allowRemote := false
	err := ValidateExecution(ResolvedConfig{
		AllowRemote: &allowRemote,
		Config:      Config{Provider: ProviderCodexHeadlessOAuth},
	}, true)
	if err == nil {
		t.Fatalf("expected config-level remote postprocess rejection")
	}
	if got := err.Error(); got == "" || !strings.Contains(got, "config forbids") {
		t.Fatalf("expected config rejection, got %v", err)
	}
}
