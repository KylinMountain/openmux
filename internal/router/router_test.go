package router

import (
	"testing"
	"github.com/openmux/openmux/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRouter_DefaultPassthrough(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.ProviderConfig{
			"zhipu": {
				APIKeys: []string{"key1"},
			},
		},
	}

	router := NewRouter(cfg)

	selector, err := router.Route("zhipu/glm-4-flash")
	require.NoError(t, err)
	require.NotNil(t, selector)

	targets := selector.GetAll()
	require.Len(t, targets, 1)

	assert.Equal(t, "zhipu", targets[0].Provider)
	assert.Equal(t, "glm-4-flash", targets[0].Model)
}

func TestRouter_ModelRoutePriority(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.ProviderConfig{
			"provider-a": {
				APIKeys: []string{"key-a"},
			},
			"provider-b": {
				APIKeys: []string{"key-b"},
			},
		},
		ModelRoutes: map[string]config.ModelRouteConfig{
			"my-virtual-model": {
				Targets: []config.Target{
					{Provider: "provider-a", Model: "model-a"},
				},
			},
		},
	}

	router := NewRouter(cfg)

	selector, err := router.Route("my-virtual-model")
	require.NoError(t, err)
	require.NotNil(t, selector)

	targets := selector.GetAll()
	require.Len(t, targets, 1)
	assert.Equal(t, "provider-a", targets[0].Provider)
	assert.Equal(t, "model-a", targets[0].Model)
}

func TestRouter_UnknownProviderPassthrough(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.ProviderConfig{
			"zhipu": {
				APIKeys: []string{"key1"},
			},
		},
	}

	router := NewRouter(cfg)

	_, err := router.Route("unknown-provider/some-model")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "provider not found")
}

func TestRouter_ModelRouteWithMultipleTargets(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.ProviderConfig{
			"provider-a": { APIKeys: []string{"key-a"} },
			"provider-b": { APIKeys: []string{"key-b"} },
		},
		ModelRoutes: map[string]config.ModelRouteConfig{
			"balanced-model": {
				Targets: []config.Target{
					{Provider: "provider-a", Model: "model-a"},
					{Provider: "provider-b", Model: "model-b"},
				},
			},
		},
	}

	router := NewRouter(cfg)
	selector, err := router.Route("balanced-model")
	require.NoError(t, err)
	
	targets := selector.GetAll()
	require.Len(t, targets, 2)
	
	// Check if both targets are present in the selector
	foundA := false
	foundB := false
	for _, target := range targets {
		if target.Provider == "provider-a" {
			foundA = true
		}
		if target.Provider == "provider-b" {
			foundB = true
		}
	}
	assert.True(t, foundA, "provider-a should be a target")
	assert.True(t, foundB, "provider-b should be a target")
}

func TestPrioritySelectorKeepsConfigOrder(t *testing.T) {
	targets := []config.Target{
		{Provider: "a", Model: "m1"},
		{Provider: "b", Model: "m2"},
		{Provider: "c", Model: "m3"},
	}
	sel := NewPriorityTargetSelector(targets)
	for i := 0; i < 5; i++ {
		got, err := sel.Select()
		if err != nil || got.Provider != "a" {
			t.Fatalf("Select #%d = %v, %v; want provider a", i, got, err)
		}
	}
	all := sel.GetAll()
	for i, want := range []string{"a", "b", "c"} {
		if all[i].Provider != want {
			t.Fatalf("GetAll()[%d] = %s, want %s", i, all[i].Provider, want)
		}
	}
	if _, err := NewPriorityTargetSelector(nil).Select(); err == nil {
		t.Fatal("empty selector should return an error")
	}
}
