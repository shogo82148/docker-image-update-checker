package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/shogo82148/docker-image-update-checker/registry"
)

var targets = []string{
	// alpine
	"alpine:3.15",
	"alpine:3.14",
	"alpine:3.13",
	"alpine:3.12",
	"alpine:3.11",

	// debian
	"buildpack-deps:bullseye",
	"buildpack-deps:buster",
	"debian:bookworm-slim",
	"debian:bullseye-slim",
	"debian:buster-slim",

	// ubuntu
	"ubuntu:18.04",
	"ubuntu:20.04",

	// amazonlinux
	"amazonlinux:2",

	// images for AWS Lambda
	"amazon/aws-lambda-provided:al2",
	"amazon/aws-lambda-provided:alami",
	"lambci/lambda:build-provided",
	"lambci/lambda:build-provided.al2",
	"lambci/lambda:provided",
	"lambci/lambda:provided.al2",
}

var status map[string]*registry.Manifests
var updated map[string]struct{}

func loadStatus() error {
	status = map[string]*registry.Manifests{}
	for _, image := range targets {
		host, repo, tag := registry.GetRepository(image)
		statusFile := filepath.FromSlash("manifests/" + host + "/" + repo + "/" + tag + ".json")
		data, err := os.ReadFile(statusFile)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}

		var manifests *registry.Manifests
		if err := json.Unmarshal(data, &manifests); err != nil {
			continue
		}
		status[image] = manifests
	}
	return nil
}

func saveStatus() error {
	for image := range updated {
		host, repo, tag := registry.GetRepository(image)
		statusFile := filepath.FromSlash("manifests/" + host + "/" + repo + "/" + tag + ".json")
		if err := os.MkdirAll(filepath.Dir(statusFile), 0755); err != nil {
			return err
		}
		data, err := json.MarshalIndent(status[image], "", "    ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(statusFile, data, 0644); err != nil {
			return err
		}
	}
	return commit()
}

func checkUpdates() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	c := registry.New()
	for _, image := range targets {
		if err := checkUpdate(ctx, c, image); err != nil {
			log.Printf("failed to get %s: %v", image, err)
		}
	}
}

func checkUpdate(ctx context.Context, c *registry.Client, image string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	log.Printf("getting manifest: %s", image)
	m, err := c.GetManifests(ctx, image)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(status[image], m) {
		log.Printf("updated: %s", image)
		updated[image] = struct{}{}
	}
	status[image] = m
	return nil
}

func commit() error {
	if len(updated) == 0 {
		return nil
	}
	updates := make([]string, 0, len(updated))
	for image := range updated {
		updates = append(updates, image)
	}
	sort.Strings(updates)

	git, err := exec.LookPath("git")
	if err != nil {
		return err
	}
	commands := []struct {
		cmd  string
		args []string
	}{
		{git, []string{"config", "--local", "user.name", "Ichinose Shogo"}},
		{git, []string{"config", "--local", "user.email", "shogo82148@gmail.com"}},
		{git, []string{"add", "."}},
		{git, []string{"commit", "-m", "update: " + strings.Join(updates, ", ")}},
		{git, []string{"push", "origin", "main"}},
	}
	for _, command := range commands {
		if err := exec.Command(command.cmd, command.args...).Run(); err != nil {
			return err
		}
	}
	return nil
}

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)

	updated = map[string]struct{}{}
	if err := loadStatus(); err != nil {
		log.Fatalf("failed to load status: %v", err)
	}

	checkUpdates()

	if err := saveStatus(); err != nil {
		log.Fatalf("failed to save status: %v", err)
	}
}
