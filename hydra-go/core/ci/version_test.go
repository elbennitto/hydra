package ci

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseChartVersion(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected ChartVersion
		wantErr  bool
	}{
		{
			name:     "v prefix major dev",
			input:    "v4.11.0-dev",
			expected: ChartVersion{Major: 4, Minor: 11, Patch: 0, Extra: -1, Env: "dev"},
		},
		{
			name:     "standard dev version",
			input:    "1.200.9-dev",
			expected: ChartVersion{Major: 1, Minor: 200, Patch: 9, Extra: -1, Env: "dev"},
		},
		{
			name:     "standard stage version",
			input:    "1.200.9-stage",
			expected: ChartVersion{Major: 1, Minor: 200, Patch: 9, Extra: -1, Env: "stage"},
		},
		{
			name:     "standard prod version",
			input:    "1.200.9",
			expected: ChartVersion{Major: 1, Minor: 200, Patch: 9, Extra: -1, Env: ""},
		},
		{
			name:     "extra version dev",
			input:    "1.200.9-2-dev",
			expected: ChartVersion{Major: 1, Minor: 200, Patch: 9, Extra: 2, Env: "dev"},
		},
		{
			name:     "extra version stage",
			input:    "1.200.9-1-stage",
			expected: ChartVersion{Major: 1, Minor: 200, Patch: 9, Extra: 1, Env: "stage"},
		},
		{
			name:     "extra version prod",
			input:    "1.200.9-3",
			expected: ChartVersion{Major: 1, Minor: 200, Patch: 9, Extra: 3, Env: ""},
		},
		{
			name:     "sprint version dev",
			input:    "42.0.0-dev",
			expected: ChartVersion{Major: 42, Minor: 0, Patch: 0, Extra: -1, Env: "dev"},
		},
		{
			name:    "invalid format",
			input:   "not-a-version",
			wantErr: true,
		},
		{
			name:    "missing patch",
			input:   "1.200",
			wantErr: true,
		},
		{
			name:     "prerelease dev",
			input:    "1.5.1-2b866935-dev",
			expected: ChartVersion{Major: 1, Minor: 5, Patch: 1, PreRelease: "2b866935", Extra: -1, Env: "dev"},
		},
		{
			name:     "prerelease stage",
			input:    "1.5.1-2b866935-stage",
			expected: ChartVersion{Major: 1, Minor: 5, Patch: 1, PreRelease: "2b866935", Extra: -1, Env: "stage"},
		},
		{
			name:     "prerelease only",
			input:    "1.5.1-2b866935",
			expected: ChartVersion{Major: 1, Minor: 5, Patch: 1, PreRelease: "2b866935", Extra: -1, Env: ""},
		},
		{
			name:     "prerelease extra dev",
			input:    "1.5.1-2b866935-1-dev",
			expected: ChartVersion{Major: 1, Minor: 5, Patch: 1, PreRelease: "2b866935", Extra: 1, Env: "dev"},
		},
		{
			name:     "prerelease extra stage",
			input:    "1.5.1-2b866935-2-stage",
			expected: ChartVersion{Major: 1, Minor: 5, Patch: 1, PreRelease: "2b866935", Extra: 2, Env: "stage"},
		},
		{
			name:     "prerelease extra no env",
			input:    "1.5.1-2b866935-3",
			expected: ChartVersion{Major: 1, Minor: 5, Patch: 1, PreRelease: "2b866935", Extra: 3, Env: ""},
		},
		{
			name:     "alpha prerelease dev",
			input:    "1.5.1-alpha-dev",
			expected: ChartVersion{Major: 1, Minor: 5, Patch: 1, PreRelease: "alpha", Extra: -1, Env: "dev"},
		},
		{
			name:     "rc1 prerelease extra stage",
			input:    "1.0.0-rc1-2-stage",
			expected: ChartVersion{Major: 1, Minor: 0, Patch: 0, PreRelease: "rc1", Extra: 2, Env: "stage"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v, err := ParseChartVersion(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.expected, v)
		})
	}
}

func TestChartVersion_String(t *testing.T) {
	tests := []struct {
		name     string
		version  ChartVersion
		expected string
	}{
		{
			name:     "dev version",
			version:  ChartVersion{Major: 1, Minor: 200, Patch: 9, Extra: -1, Env: "dev"},
			expected: "1.200.9-dev",
		},
		{
			name:     "prod version",
			version:  ChartVersion{Major: 1, Minor: 200, Patch: 9, Extra: -1, Env: ""},
			expected: "1.200.9",
		},
		{
			name:     "extra dev version",
			version:  ChartVersion{Major: 1, Minor: 200, Patch: 9, Extra: 2, Env: "dev"},
			expected: "1.200.9-2-dev",
		},
		{
			name:     "extra prod version",
			version:  ChartVersion{Major: 1, Minor: 200, Patch: 9, Extra: 1, Env: ""},
			expected: "1.200.9-1",
		},
		{
			name:     "prerelease dev",
			version:  ChartVersion{Major: 1, Minor: 5, Patch: 1, PreRelease: "2b866935", Extra: -1, Env: "dev"},
			expected: "1.5.1-2b866935-dev",
		},
		{
			name:     "prerelease no env",
			version:  ChartVersion{Major: 1, Minor: 5, Patch: 1, PreRelease: "2b866935", Extra: -1, Env: ""},
			expected: "1.5.1-2b866935",
		},
		{
			name:     "prerelease extra dev",
			version:  ChartVersion{Major: 1, Minor: 5, Patch: 1, PreRelease: "2b866935", Extra: 1, Env: "dev"},
			expected: "1.5.1-2b866935-1-dev",
		},
		{
			name:     "prerelease extra no env",
			version:  ChartVersion{Major: 1, Minor: 5, Patch: 1, PreRelease: "2b866935", Extra: 2, Env: ""},
			expected: "1.5.1-2b866935-2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.version.String())
		})
	}
}

func TestChartVersion_BaseVersion(t *testing.T) {
	v := ChartVersion{Major: 1, Minor: 200, Patch: 9, Extra: 2, Env: "dev"}
	assert.Equal(t, "1.200.9", v.BaseVersion())

	vNoPreRelease := ChartVersion{Major: 1, Minor: 200, Patch: 9, PreRelease: "", Extra: 2, Env: "dev"}
	assert.Equal(t, "1.200.9", vNoPreRelease.BaseVersion())

	vPreRelease := ChartVersion{Major: 1, Minor: 5, Patch: 1, PreRelease: "2b866935", Extra: 2, Env: "dev"}
	assert.Equal(t, "1.5.1-2b866935", vPreRelease.BaseVersion())
}

func TestComputeWrapperVersion(t *testing.T) {
	tests := []struct {
		name        string
		depVersion  string
		env         string
		extraExists int
		expected    string
		wantErr     bool
	}{
		{
			name:        "standard dev",
			depVersion:  "1.200.9",
			env:         "dev",
			extraExists: -1,
			expected:    "1.200.9-dev",
		},
		{
			name:        "standard stage",
			depVersion:  "1.200.9",
			env:         "stage",
			extraExists: -1,
			expected:    "1.200.9-stage",
		},
		{
			name:        "standard prod",
			depVersion:  "1.200.9",
			env:         "prod",
			extraExists: -1,
			expected:    "1.200.9",
		},
		{
			name:        "v prefix dependency dev",
			depVersion:  "v4.11.0",
			env:         "dev",
			extraExists: -1,
			expected:    "4.11.0-dev",
		},
		{
			name:        "V prefix dependency with prerelease dev",
			depVersion:  "V1.5.1-2b866935",
			env:         "dev",
			extraExists: -1,
			expected:    "1.5.1-2b866935-dev",
		},
		{
			name:        "first extra dev",
			depVersion:  "1.200.9",
			env:         "dev",
			extraExists: 0,
			expected:    "1.200.9-1-dev",
		},
		{
			name:        "second extra dev",
			depVersion:  "1.200.9",
			env:         "dev",
			extraExists: 1,
			expected:    "1.200.9-2-dev",
		},
		{
			name:        "first extra prod",
			depVersion:  "1.200.9",
			env:         "prod",
			extraExists: 0,
			expected:    "1.200.9-1",
		},
		{
			name:        "unknown env",
			depVersion:  "1.200.9",
			env:         "unknown",
			extraExists: -1,
			wantErr:     true,
		},
		{
			name:        "invalid dep version",
			depVersion:  "invalid",
			env:         "dev",
			extraExists: -1,
			wantErr:     true,
		},
		{
			name:        "prerelease dev",
			depVersion:  "1.5.1-2b866935",
			env:         "dev",
			extraExists: -1,
			expected:    "1.5.1-2b866935-dev",
		},
		{
			name:        "prerelease stage",
			depVersion:  "1.5.1-2b866935",
			env:         "stage",
			extraExists: -1,
			expected:    "1.5.1-2b866935-stage",
		},
		{
			name:        "prerelease prod",
			depVersion:  "1.5.1-2b866935",
			env:         "prod",
			extraExists: -1,
			expected:    "1.5.1-2b866935",
		},
		{
			name:        "prerelease first extra dev",
			depVersion:  "1.5.1-2b866935",
			env:         "dev",
			extraExists: 0,
			expected:    "1.5.1-2b866935-1-dev",
		},
		{
			name:        "prerelease third extra dev",
			depVersion:  "1.5.1-2b866935",
			env:         "dev",
			extraExists: 2,
			expected:    "1.5.1-2b866935-3-dev",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ComputeWrapperVersion(tt.depVersion, tt.env, tt.extraExists)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestComputePromoteTargetVersion(t *testing.T) {
	tests := []struct {
		name                  string
		sourceVersion         string
		sourceEnv             string
		targetEnv             string
		existingTargetVersion string
		expected              string
		wantErr               bool
	}{
		{
			name:                  "dev to stage no existing dir",
			sourceVersion:         "1.200.9-dev",
			sourceEnv:             "dev",
			targetEnv:             "stage",
			existingTargetVersion: "",
			expected:              "1.200.9-stage",
		},
		{
			name:                  "stage to prod no existing",
			sourceVersion:         "1.200.9-stage",
			sourceEnv:             "stage",
			targetEnv:             "prod",
			existingTargetVersion: "",
			expected:              "1.200.9",
		},
		{
			name:                  "extra dev to stage base slot free",
			sourceVersion:         "1.200.9-2-dev",
			sourceEnv:             "dev",
			targetEnv:             "stage",
			existingTargetVersion: "1.200.9-1-stage",
			expected:              "1.200.9-stage",
		},
		{
			name:                  "extra dev to stage base slot occupied",
			sourceVersion:         "1.0.0-2-dev",
			sourceEnv:             "dev",
			targetEnv:             "stage",
			existingTargetVersion: "1.0.0-stage",
			expected:              "1.0.0-1-stage",
		},
		{
			name:                  "dev without extra target already base line",
			sourceVersion:         "1.200.9-dev",
			sourceEnv:             "dev",
			targetEnv:             "stage",
			existingTargetVersion: "1.200.9-stage",
			expected:              "1.200.9-stage",
		},
		{
			name:                  "extra stage to prod base slot free",
			sourceVersion:         "1.200.9-2-stage",
			sourceEnv:             "stage",
			targetEnv:             "prod",
			existingTargetVersion: "1.200.9-1",
			expected:              "1.200.9",
		},
		{
			name:                  "extra stage to prod base slot occupied",
			sourceVersion:         "1.200.9-1-stage",
			sourceEnv:             "stage",
			targetEnv:             "prod",
			existingTargetVersion: "1.200.9",
			expected:              "1.200.9-1",
		},
		{
			name:          "wrong source env",
			sourceVersion: "1.200.9-dev",
			sourceEnv:     "stage",
			targetEnv:     "prod",
			wantErr:       true,
		},
		{
			name:                  "default reset version allowed without source suffix",
			sourceVersion:         "0.0.0",
			sourceEnv:             "stage",
			targetEnv:             "prod",
			existingTargetVersion: "1.2.3",
			expected:              "0.0.0",
		},
		{
			name:                  "prerelease dev to stage",
			sourceVersion:         "1.5.1-2b866935-dev",
			sourceEnv:             "dev",
			targetEnv:             "stage",
			existingTargetVersion: "",
			expected:              "1.5.1-2b866935-stage",
		},
		{
			name:                  "prerelease stage to prod",
			sourceVersion:         "1.5.1-2b866935-stage",
			sourceEnv:             "stage",
			targetEnv:             "prod",
			existingTargetVersion: "",
			expected:              "1.5.1-2b866935",
		},
		{
			name:                  "prerelease extra dev to stage resets counter",
			sourceVersion:         "1.5.1-2b866935-1-dev",
			sourceEnv:             "dev",
			targetEnv:             "stage",
			existingTargetVersion: "",
			expected:              "1.5.1-2b866935-stage",
		},
		{
			name:                  "prerelease extra dev when base prerelease stage exists",
			sourceVersion:         "1.5.1-2b866935-2-dev",
			sourceEnv:             "dev",
			targetEnv:             "stage",
			existingTargetVersion: "1.5.1-2b866935-stage",
			expected:              "1.5.1-2b866935-1-stage",
		},
		{
			name:                  "prerelease extra stage to prod",
			sourceVersion:         "1.5.1-2b866935-2-stage",
			sourceEnv:             "stage",
			targetEnv:             "prod",
			existingTargetVersion: "1.5.1-2b866935-1",
			expected:              "1.5.1-2b866935",
		},
		{
			name:                  "invalid existing target version",
			sourceVersion:         "1.0.0-dev",
			sourceEnv:             "dev",
			targetEnv:             "stage",
			existingTargetVersion: "not-a-version",
			wantErr:               true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ComputePromoteTargetVersion(tt.sourceVersion, tt.sourceEnv, tt.targetEnv, tt.existingTargetVersion)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}
