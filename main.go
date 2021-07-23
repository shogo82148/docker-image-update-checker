package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"reflect"
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
}

const statusFile = "status.json"

var status map[string]*registry.Manifests
var updated map[string]struct{}

func loadStatus() error {
	data, err := os.ReadFile(statusFile)
	if os.IsNotExist(err) {
		status = map[string]*registry.Manifests{}
		return nil
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(data, &status)
}

func saveStatus() error {
	data, err := json.MarshalIndent(status, "", "    ")
	if err != nil {
		return err
	}
	return os.WriteFile(statusFile, data, 0644)
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
