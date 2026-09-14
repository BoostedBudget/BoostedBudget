package posthog_test

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

const supportedPostHogVersion = "v1.25.1"

type resolvedModule struct {
	Path    string
	Version string
	Replace *resolvedModule
}

func exactPostHogModule(module *resolvedModule) error {
	if module.Path != "github.com/posthog/posthog-go" {
		return fmt.Errorf("resolved unexpected PostHog module %q", module.Path)
	}
	if module.Replace != nil {
		return fmt.Errorf("PostHog SDK must not be replaced")
	}
	if module.Version != supportedPostHogVersion {
		return fmt.Errorf("resolved PostHog SDK %q, want %q", module.Version, supportedPostHogVersion)
	}
	return nil
}

func TestPostHogGoVersionIsExactSupportedRelease(t *testing.T) {
	_, sourceFile, _, ok := runtime.Caller(0)
	require.True(t, ok, "test source path must be available")
	goMod, err := os.ReadFile(filepath.Join(filepath.Dir(sourceFile), "../../../go.mod"))
	require.NoError(t, err)

	match := regexp.MustCompile(`(?m)^\s*github\.com/posthog/posthog-go (v(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)\.(?:0|[1-9]\d*))$`).FindSubmatch(goMod)
	require.Len(t, match, 2, "PostHog SDK must be a direct, exact stable dependency")

	require.Equal(t, supportedPostHogVersion, string(match[1]),
		"PostHog SDK compatibility is validated against one exact reviewed release")

	command := exec.Command("go", "list", "-mod=readonly", "-m", "-json", "github.com/posthog/posthog-go")
	command.Dir = filepath.Join(filepath.Dir(sourceFile), "../../..")
	output, err := command.Output()
	require.NoError(t, err)
	var module resolvedModule
	require.NoError(t, json.Unmarshal(output, &module))
	require.NoError(t, exactPostHogModule(&module))
}

func TestExactPostHogModuleRejectsReplacement(t *testing.T) {
	err := exactPostHogModule(&resolvedModule{
		Path:    "github.com/posthog/posthog-go",
		Version: supportedPostHogVersion,
		Replace: &resolvedModule{Path: "../local-fork"},
	})
	require.ErrorContains(t, err, "must not be replaced")
}
