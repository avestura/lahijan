package conf

import (
	"testing"

	"github.com/spf13/viper"
)

// TestSetupConfigBindsNestedEnv guards the env key replacer: the compose
// stacks configure nested keys purely through LAHIJAN_* env vars.
func TestSetupConfigBindsNestedEnv(t *testing.T) {
	// Not parallel: t.Setenv and the global Viper instance.
	t.Setenv("LAHIJAN_PROVIDERS_SEAWEEDFS_ENABLED", "true")
	t.Setenv("LAHIJAN_PROVIDERS_SEAWEEDFS_ADMINACCESSKEY", "from-env")
	t.Cleanup(viper.Reset)

	if _, err := SetupConfig(); err != nil {
		t.Fatalf("SetupConfig: %v", err)
	}
	if !viper.GetBool("providers.seaweedfs.enabled") {
		t.Errorf("providers.seaweedfs.enabled = false, want true from env")
	}
	if got := viper.GetString("providers.seaweedfs.adminAccessKey"); got != "from-env" {
		t.Errorf("providers.seaweedfs.adminAccessKey = %q, want %q", got, "from-env")
	}
}
