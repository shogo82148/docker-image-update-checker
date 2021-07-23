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
	"alpine:3.11",
	"alpine:3.12",
	"alpine:3.13",
	"alpine:3.14",
	"buildpack-deps:bullseye",
	"buildpack-deps:buster",
	"debian:bullseye-slim",
	"debian:buster-slim",
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
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c := registry.New()
	for _, image := range targets {
		log.Printf("getting manifest: %s", image)
		m, err := c.GetManifests(ctx, image)
		if err != nil {
			continue
		}
		if !reflect.DeepEqual(status[image], m) {
			log.Printf("updated: %s", image)
			updated[image] = struct{}{}
		}
		status[image] = m
	}
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
	updated = map[string]struct{}{}
	if err := loadStatus(); err != nil {
		log.Fatalf("failed to load status: %v", err)
	}

	checkUpdates()

	if err := saveStatus(); err != nil {
		log.Fatalf("failed to save status: %v", err)
	}
}
