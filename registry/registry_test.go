package registry

import (
	"context"
	"testing"
)

func TestGetManifests(t *testing.T) {
	var testImages []string = []string{
		"debian:latest",
		"katsubushi/katsubushi:v1.6.0",
		"public.ecr.aws/mackerel/mackerel-container-agent:plugins",
		"ghcr.io/github/super-linter:v3",
	}
	c := New()
	for _, image := range testImages {
		_, err := c.GetManifests(context.Background(), image)
		if err != nil {
			t.Errorf("failed to get %q: %v", image, err)
		}
	}
}
