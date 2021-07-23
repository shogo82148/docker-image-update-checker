package main

import (
	"context"
	"log"

	"github.com/shogo82148/docker-image-update-checker/registry"
)

func main() {
	c := registry.New()
	log.Println(c.GetManifests(context.Background(), "debian:latest"))
}
