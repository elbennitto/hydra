package hydra

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hydra-gitops.org/hydra/hydra-go/base/log"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"hydra-gitops.org/hydra/hydra-go/core/values"
)

func resetCaches() {
	contextCache = nil
	clusterCache = nil
	rootAppCache = nil
	childAppCache = nil
}

func TestResolvePath(t *testing.T) {
	// Get the testdata directory
	testdataDir, err := filepath.Abs(filepath.Join(".", "..", "..", "testdata"))
	if err != nil {
		t.Fatalf("Failed to get testdata directory: %v", err)
	}

	tests := []struct {
		name            string
		path            string
		expectedType    string
		expectedCluster types.ClusterName
		expectedRootApp types.RootAppName
		shouldError     bool
	}{
		{
			name:         "Test directory - Context",
			path:         filepath.Join(testdataDir, "test"),
			expectedType: "context",
			shouldError:  false,
		},
		{
			name:            "Test/in-cluster - Cluster",
			path:            filepath.Join(testdataDir, "test", "in-cluster"),
			expectedType:    "cluster",
			expectedCluster: "in-cluster",
			shouldError:     false,
		},
		{
			name:            "Test/in-cluster/cluster-infra - RootApp",
			path:            filepath.Join(testdataDir, "test", "in-cluster", "cluster-infra"),
			expectedType:    "rootapp",
			expectedCluster: "in-cluster",
			expectedRootApp: "cluster-infra",
			shouldError:     false,
		},
		{
			name:            "Test/target - Cluster",
			path:            filepath.Join(testdataDir, "test", "target"),
			expectedType:    "cluster",
			expectedCluster: "target",
			shouldError:     false,
		},
		{
			name:            "Test/target/cluster-infra - RootApp",
			path:            filepath.Join(testdataDir, "test", "target", "cluster-infra"),
			expectedType:    "rootapp",
			expectedCluster: "target",
			expectedRootApp: "cluster-infra",
			shouldError:     false,
		},
		{
			name:            "Test/target/example - RootApp",
			path:            filepath.Join(testdataDir, "test", "target", "example"),
			expectedType:    "rootapp",
			expectedCluster: "target",
			expectedRootApp: "example",
			shouldError:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hydra, err := ResolvePath(log.Default(), types.HydraContext(tt.path), types.NewConfig(types.ColorNo, types.DryRunYes, types.KubernetesConnectionAllowedNo, true))

			if (err != nil) != tt.shouldError {
				t.Errorf("ResolvePath() error = %v, shouldError %v", err, tt.shouldError)
				return
			}

			if err != nil {
				return
			}

			switch tt.expectedType {
			case "context":
				ctx := hydra.AsContext()
				if ctx == nil {
					t.Errorf("Expected Context, but got nil")
					return
				}
				if hydra.AsCluster() != nil {
					t.Errorf("Expected Context, but AsCluster() returned non-nil")
				}
				if hydra.AsRootApp() != nil {
					t.Errorf("Expected Context, but AsRootApp() returned non-nil")
				}

			case "cluster":
				cluster := hydra.AsCluster()
				if cluster == nil {
					t.Errorf("Expected Cluster, but got nil")
					return
				}
				if cluster.ClusterName != tt.expectedCluster {
					t.Errorf("Expected ClusterName %q, but got %q", tt.expectedCluster, cluster.ClusterName)
				}
				if hydra.AsContext() == nil {
					t.Errorf("Expected AsContext() to return non-nil for Cluster")
				}
				if hydra.AsRootApp() != nil {
					t.Errorf("Expected Cluster, but AsRootApp() returned non-nil")
				}

			case "rootapp":
				rootApp := hydra.AsRootApp()
				if rootApp == nil {
					t.Errorf("Expected RootApp, but got nil")
					return
				}
				if rootApp.ClusterName != tt.expectedCluster {
					t.Errorf("Expected ClusterName %q, but got %q", tt.expectedCluster, rootApp.ClusterName)
				}
				if rootApp.RootAppName != tt.expectedRootApp {
					t.Errorf("Expected RootAppName %q, but got %q", tt.expectedRootApp, rootApp.RootAppName)
				}
				if hydra.AsContext() == nil {
					t.Errorf("Expected AsContext() to return non-nil for RootApp")
				}
				if hydra.AsCluster() == nil {
					t.Errorf("Expected AsCluster() to return non-nil for RootApp")
				}
			}
		})
	}
}

func TestContextCacheRespectsKubernetesConnectionAllowed(t *testing.T) {
	resetCaches()
	defer resetCaches()

	testdataDir, err := filepath.Abs(filepath.Join(".", "..", "..", "testdata"))
	if err != nil {
		t.Fatalf("Failed to get testdata directory: %v", err)
	}
	testPath := filepath.Join(testdataDir, "test")

	offlineConfig := types.NewConfig(types.ColorNo, types.DryRunNo, types.KubernetesConnectionAllowedNo, true)
	ctx1, err := ResolvePath(log.Default(), types.HydraContext(testPath), offlineConfig)
	if err != nil {
		t.Fatalf("ResolvePath with KubernetesConnectionAllowedNo failed: %v", err)
	}
	if ctx1.Config().KubernetesConnectionAllowed() != types.KubernetesConnectionAllowedNo {
		t.Fatal("Expected KubernetesConnectionAllowedNo for first context")
	}

	onlineConfig := types.NewConfig(types.ColorNo, types.DryRunNo, types.KubernetesConnectionAllowedYes, true)
	ctx2, err := ResolvePath(log.Default(), types.HydraContext(testPath), onlineConfig)
	if err != nil {
		t.Fatalf("ResolvePath with KubernetesConnectionAllowedYes failed: %v", err)
	}
	if ctx2.Config().KubernetesConnectionAllowed() != types.KubernetesConnectionAllowedYes {
		t.Fatalf("Expected KubernetesConnectionAllowedYes for second context, but got KubernetesConnectionAllowedNo (cache returned stale config)")
	}
}

func TestResolvePath_ContextWithoutInClusterArgocdAccepted(t *testing.T) {
	resetCaches()
	defer resetCaches()

	root := t.TempDir()
	requireNoError(t, os.WriteFile(filepath.Join(root, "values.yaml"), []byte("global:\n  hydra:\n    type: context\n"), 0o644))
	clusterValues := filepath.Join(root, "cluster-a", "values.yaml")
	requireNoError(t, os.MkdirAll(filepath.Dir(clusterValues), 0o755))
	requireNoError(t, os.WriteFile(clusterValues, []byte("global:\n  hydra:\n    type: cluster\n"), 0o644))

	hydra, err := ResolvePath(log.Default(), types.HydraContext(root), types.NewConfig(types.ColorNo, types.DryRunYes, types.KubernetesConnectionAllowedNo, true))
	if err != nil {
		t.Fatalf("ResolvePath() unexpected error: %v", err)
	}
	if hydra.AsContext() == nil {
		t.Fatalf("expected context, got non-context result")
	}
}

func TestResolvePath_TypeConflictBetweenParentAndChildFails(t *testing.T) {
	resetCaches()
	defer resetCaches()

	group := t.TempDir()
	context := filepath.Join(group, "ctx")
	requireNoError(t, os.MkdirAll(context, 0o755))
	requireNoError(t, os.WriteFile(filepath.Join(group, "values.yaml"), []byte("global:\n  hydra:\n    type: group\n"), 0o644))
	requireNoError(t, os.WriteFile(filepath.Join(context, "values.yaml"), []byte("global:\n  hydra:\n    type: cluster\n"), 0o644))

	_, err := CreateContext(log.Default(), context, types.NewConfig(types.ColorNo, types.DryRunYes, types.KubernetesConnectionAllowedNo, true))
	if err == nil {
		t.Fatal("expected error when child type conflicts with inherited type")
	}
	if !strings.Contains(err.Error(), "conflicting") && !strings.Contains(err.Error(), "expected") {
		t.Fatalf("expected type conflict error, got: %v", err)
	}
}

func TestResolvePath_RequiresTypeOnAtLeastOneLevel(t *testing.T) {
	resetCaches()
	defer resetCaches()

	root := t.TempDir()
	_, err := CreateContext(log.Default(), root, types.NewConfig(types.ColorNo, types.DryRunYes, types.KubernetesConnectionAllowedNo, true))
	if err == nil {
		t.Fatal("expected error when no level defines global.hydra.type")
	}
}

func TestResolvePath_ContextErrorIncludesHintWhenDirectoryIsMissing(t *testing.T) {
	resetCaches()
	defer resetCaches()

	root := t.TempDir()
	clusterDir := filepath.Join(root, "clusters", "test", "frobel")

	_, err := ResolvePath(log.Default(), types.HydraContext(clusterDir), types.NewConfig(types.ColorNo, types.DryRunYes, types.KubernetesConnectionAllowedNo, true))
	if err == nil {
		t.Fatal("expected error when path does not resolve to a Hydra context")
	}

	msg := err.Error()
	if !strings.Contains(msg, "failed to use Hydra context path") {
		t.Fatalf("expected outer error message to mention Hydra context path, got: %v", err)
	}
	if !strings.Contains(msg, "directory does not exist") {
		t.Fatalf("expected missing-directory hint in error message, got: %v", err)
	}
}

func TestResolvePath_ContextErrorIncludesHintWhenValuesFileIsMissing(t *testing.T) {
	resetCaches()
	defer resetCaches()

	root := t.TempDir()
	clusterDir := filepath.Join(root, "clusters", "test", "frobel")
	requireNoError(t, os.MkdirAll(clusterDir, 0o755))

	_, err := ResolvePath(log.Default(), types.HydraContext(clusterDir), types.NewConfig(types.ColorNo, types.DryRunYes, types.KubernetesConnectionAllowedNo, true))
	if err == nil {
		t.Fatal("expected error when values.yaml is missing")
	}

	msg := err.Error()
	if !strings.Contains(msg, filepath.Join(clusterDir, "values.yaml")) {
		t.Fatalf("expected missing values.yaml hint in error message, got: %v", err)
	}
	if !strings.Contains(msg, "Missing file:") {
		t.Fatalf("expected missing values.yaml wording in error message, got: %v", err)
	}
	if !strings.Contains(msg, "Recommended: set 'global.hydra.type: group'") {
		t.Fatalf("expected recommended group fix in error message, got: %v", err)
	}
}

func TestResolvePath_ContextErrorIncludesHintWhenTypeIsNotContext(t *testing.T) {
	resetCaches()
	defer resetCaches()

	root := t.TempDir()
	clusterDir := filepath.Join(root, "clusters", "test", "frobel")
	requireNoError(t, os.MkdirAll(clusterDir, 0o755))
	requireNoError(t, os.WriteFile(filepath.Join(clusterDir, "values.yaml"), []byte("global:\n  hydra:\n    type: cluster\n    parent: false\n"), 0o644))

	_, err := ResolvePath(log.Default(), types.HydraContext(clusterDir), types.NewConfig(types.ColorNo, types.DryRunYes, types.KubernetesConnectionAllowedNo, true))
	if err == nil {
		t.Fatal("expected error when values.yaml declares non-context hydra type")
	}

	msg := err.Error()
	if !strings.Contains(msg, "change '") || !strings.Contains(msg, "global.hydra.type: cluster") {
		t.Fatalf("expected non-context type hint in error message, got: %v", err)
	}
	if !strings.Contains(msg, "Recommended: set 'global.hydra.type: group'") {
		t.Fatalf("expected recommended group fix in error message, got: %v", err)
	}
}

func TestResolvePath_ContextErrorIncludesHintWhenValuesFileHasNoContextType(t *testing.T) {
	resetCaches()
	defer resetCaches()

	root := t.TempDir()
	clusterDir := filepath.Join(root, "clusters", "test", "frobel")
	requireNoError(t, os.MkdirAll(clusterDir, 0o755))
	requireNoError(t, os.WriteFile(filepath.Join(clusterDir, "values.yaml"), []byte("global:\n  hydra:\n    parent: false\n"), 0o644))

	_, err := ResolvePath(log.Default(), types.HydraContext(clusterDir), types.NewConfig(types.ColorNo, types.DryRunYes, types.KubernetesConnectionAllowedNo, true))
	if err == nil {
		t.Fatal("expected error when values.yaml exists but no context type can be resolved")
	}

	msg := err.Error()
	if !strings.Contains(msg, "Alternative: add 'global.hydra.type: context'") {
		t.Fatalf("expected unresolved-context-type hint in error message, got: %v", err)
	}
	if !strings.Contains(msg, "Recommended: set 'global.hydra.type: group'") {
		t.Fatalf("expected recommended group fix in error message, got: %v", err)
	}
}

func TestResolvePath_GroupDerivesContextAndCluster(t *testing.T) {
	resetCaches()
	defer resetCaches()

	group := t.TempDir()
	context := filepath.Join(group, "ctx")
	requireNoError(t, os.MkdirAll(filepath.Join(context, "cluster-a"), 0o755))
	requireNoError(t, os.WriteFile(filepath.Join(group, "values.yaml"), []byte("global:\n  hydra:\n    type: group\n"), 0o644))

	ctx, err := CreateContext(log.Default(), context, types.NewConfig(types.ColorNo, types.DryRunYes, types.KubernetesConnectionAllowedNo, true))
	if err != nil {
		t.Fatalf("CreateContext() unexpected error: %v", err)
	}
	contextVals, err := ctx.LoadValuesMap(types.HelmNetworkModeOffline)
	if err != nil {
		t.Fatalf("LoadValuesMap() unexpected error: %v", err)
	}
	if got, _ := values.Lookup(contextVals, "global", "hydra", "type").(string); got != hydraTypeContext {
		t.Fatalf("expected context type %q, got %q", hydraTypeContext, got)
	}

	cluster, err := NewCluster(ctx, "cluster-a", RESTClientLimits{})
	if err != nil {
		t.Fatalf("NewCluster() unexpected error: %v", err)
	}
	clusterVals, err := cluster.LoadValuesMap(types.HelmNetworkModeOffline)
	if err != nil {
		t.Fatalf("Cluster.LoadValuesMap() unexpected error: %v", err)
	}
	if got, _ := values.Lookup(clusterVals, "global", "hydra", "type").(string); got != hydraTypeCluster {
		t.Fatalf("expected cluster type %q, got %q", hydraTypeCluster, got)
	}
}

func TestResolvePath_ParentFalsePreventsLookup(t *testing.T) {
	resetCaches()
	defer resetCaches()

	group := t.TempDir()
	context := filepath.Join(group, "ctx")
	clusterDir := filepath.Join(context, "single")
	requireNoError(t, os.MkdirAll(clusterDir, 0o755))
	requireNoError(t, os.WriteFile(filepath.Join(group, "values.yaml"), []byte("global:\n  hydra:\n    type: group\n"), 0o644))
	requireNoError(t, os.WriteFile(filepath.Join(clusterDir, "values.yaml"), []byte("global:\n  hydra:\n    type: cluster\n    parent: false\n"), 0o644))

	if _, err := CreateContext(log.Default(), clusterDir, types.NewConfig(types.ColorNo, types.DryRunYes, types.KubernetesConnectionAllowedNo, true)); err == nil {
		t.Fatal("expected context resolution failure when parent lookup is disabled at cluster level")
	}
}

func TestResolvePath_GroupDefaultsToParentFalse(t *testing.T) {
	resetCaches()
	defer resetCaches()

	top := t.TempDir()
	group := filepath.Join(top, "group")
	context := filepath.Join(group, "ctx")
	requireNoError(t, os.MkdirAll(filepath.Join(context, "cluster-a"), 0o755))

	// This would conflict if lookup continued above the group level.
	requireNoError(t, os.WriteFile(filepath.Join(top, "values.yaml"), []byte("global:\n  hydra:\n    type: context\n"), 0o644))
	requireNoError(t, os.WriteFile(filepath.Join(group, "values.yaml"), []byte("global:\n  hydra:\n    type: group\n"), 0o644))

	if _, err := CreateContext(log.Default(), context, types.NewConfig(types.ColorNo, types.DryRunYes, types.KubernetesConnectionAllowedNo, true)); err != nil {
		t.Fatalf("expected successful context resolution with group default parent:false, got: %v", err)
	}
}

func TestHydraTypePropagation_RootAndChildValues(t *testing.T) {
	resetCaches()
	defer resetCaches()

	contextDir, _ := writeDiskCacheTestGitopsLayout(t)
	cfg := types.NewConfig(types.ColorNo, types.DryRunNo, types.KubernetesConnectionAllowedNo, true)
	ctx, err := NewContext(log.Default(), types.ContextPath(contextDir), cfg)
	if err != nil {
		t.Fatalf("NewContext() unexpected error: %v", err)
	}
	cluster, err := NewCluster(ctx, "target", RESTClientLimits{})
	if err != nil {
		t.Fatalf("NewCluster() unexpected error: %v", err)
	}

	root, err := NewRootApp(cluster, "platform")
	if err != nil {
		t.Fatalf("NewRootApp() unexpected error: %v", err)
	}
	rootVals, err := root.LoadValuesMap(types.HelmNetworkModeLocal)
	if err != nil {
		t.Fatalf("RootApp.LoadValuesMap() unexpected error: %v", err)
	}
	if got, _ := values.Lookup(rootVals, "global", "hydra", "type").(string); got != hydraTypeRootApp {
		t.Fatalf("expected root-app type %q, got %q", hydraTypeRootApp, got)
	}

	app, err := cluster.WithApp("target.platform.beta")
	if err != nil {
		t.Fatalf("WithApp() unexpected error: %v", err)
	}
	child := app.AsChildApp()
	if child == nil {
		t.Fatal("expected child app")
	}
	childVals, err := child.LoadValuesMap(types.HelmNetworkModeLocal)
	if err != nil {
		t.Fatalf("ChildApp.LoadValuesMap() unexpected error: %v", err)
	}
	if got, _ := values.Lookup(childVals, "global", "hydra", "type").(string); got != hydraTypeChildApp {
		t.Fatalf("expected child-app type %q, got %q", hydraTypeChildApp, got)
	}
}

func requireNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
