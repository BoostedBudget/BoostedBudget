package posthog_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

const minimumPostHogVersion = "v1.24.3"

var stableVersionPattern = regexp.MustCompile(`^v(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$`)

func TestPostHogGoVersionMeetsSupportedFloor(t *testing.T) {
	_, sourceFile, _, ok := runtime.Caller(0)
	require.True(t, ok, "test source path must be available")
	goMod, err := os.ReadFile(filepath.Join(filepath.Dir(sourceFile), "../../../go.mod"))
	require.NoError(t, err)

	match := regexp.MustCompile(`(?m)^\s*github\.com/posthog/posthog-go (v(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)\.(?:0|[1-9]\d*))$`).FindSubmatch(goMod)
	require.Len(t, match, 2, "PostHog SDK must be a direct, exact stable dependency")

	version, err := parseStableVersion(string(match[1]))
	require.NoError(t, err)
	minimum, err := parseStableVersion(minimumPostHogVersion)
	require.NoError(t, err)
	require.True(t, versionAtLeast(version, minimum),
		"PostHog SDK must be %s or newer", minimumPostHogVersion)
}

func TestPostHogVersionComparison(t *testing.T) {
	minimum, err := parseStableVersion("v1.24.3")
	require.NoError(t, err)

	for _, test := range []struct {
		version string
		want    bool
	}{
		{"v1.24.2", false},
		{"v1.24.3", true},
		{"v1.24.4", true},
		{"v1.25.0", true},
		{"v2.0.0", true},
	} {
		t.Run(test.version, func(t *testing.T) {
			version, err := parseStableVersion(test.version)
			require.NoError(t, err)
			require.Equal(t, test.want, versionAtLeast(version, minimum))
		})
	}
}

func TestParseStableVersionRejectsLeadingZeros(t *testing.T) {
	for _, version := range []string{"v01.24.3", "v1.024.3", "v1.24.03"} {
		t.Run(version, func(t *testing.T) {
			_, err := parseStableVersion(version)
			require.Error(t, err)
		})
	}
}

func versionAtLeast(version, minimum [3]int) bool {
	for index := range version {
		if version[index] != minimum[index] {
			return version[index] > minimum[index]
		}
	}
	return true
}

func parseStableVersion(version string) ([3]int, error) {
	match := stableVersionPattern.FindStringSubmatch(version)
	if match == nil {
		return [3]int{}, fmt.Errorf("invalid stable SemVer %q", version)
	}
	var parsed [3]int
	for index, component := range match[1:] {
		value, err := strconv.Atoi(component)
		if err != nil {
			return [3]int{}, fmt.Errorf("parse stable SemVer %q: %w", version, err)
		}
		parsed[index] = value
	}
	return parsed, nil
}
