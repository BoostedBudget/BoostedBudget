package posthog_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPostHogGoVersionMeetsSupportedFloor(t *testing.T) {
	_, sourceFile, _, ok := runtime.Caller(0)
	require.True(t, ok, "test source path must be available")
	goMod, err := os.ReadFile(filepath.Join(filepath.Dir(sourceFile), "../../../go.mod"))
	require.NoError(t, err)

	const minimumVersion = "v1.24.3"
	match := regexp.MustCompile(`(?m)^\s*github\.com/posthog/posthog-go (v\d+\.\d+\.\d+)$`).FindSubmatch(goMod)
	require.Len(t, match, 2, "PostHog SDK must be a direct, exact stable dependency")

	var major, minor, patch int
	parsed, err := fmt.Sscanf(string(match[1]), "v%d.%d.%d", &major, &minor, &patch)
	require.NoError(t, err)
	require.Equal(t, 3, parsed)
	require.True(t, versionAtLeast([3]int{major, minor, patch}, [3]int{1, 24, 3}),
		"PostHog SDK must be %s or newer", minimumVersion)
}

func versionAtLeast(version, minimum [3]int) bool {
	for index := range version {
		if version[index] != minimum[index] {
			return version[index] > minimum[index]
		}
	}
	return true
}
