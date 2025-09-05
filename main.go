package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"strings"
	"time"

	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
)

var BuildVersion = "dev"

func main() {
	cfg := flag.String("config", "config.json", "config file")
	help := flag.Bool("help", false, "show help")
	flag.Parse()

	if *help {
		fmt.Printf("Version: %s\n", BuildVersion)
		flag.Usage()
		return
	}

	config, err := loadConfig(*cfg)
	if err != nil {
		log.Fatal(err)
	}

	cli, err := client.NewClientWithOpts(
		client.FromEnv,
		client.WithAPIVersionNegotiation(),
	)
	if err != nil {
		log.Fatalf("Failed to create Docker client: %v", err)
	}
	defer func() {
		_ = cli.Close()
	}()

	for {
		if e := processImages(cli, config); e != nil {
			log.Printf("Error processing images: %v", e)
		}

		if !config.DisablePrune {
			if e := pruneUnusedImages(cli); e != nil {
				log.Printf("Error pruning unused images: %v", e)
			}
		}

		if newConfig, e := loadConfig(*cfg); e == nil {
			config = newConfig
		}

		log.Printf("Sleeping for %d seconds", config.Duration)
		time.Sleep(time.Duration(config.Duration) * time.Second)
	}
}

func processImages(cli *client.Client, config *Config) error {
	for _, img := range config.Images {
		pull := image.PullOptions{
			All: true,
		}
		push := image.PushOptions{
			All: true,
		}
		if config.Auths != nil {
			for registry, auth := range config.Auths {
				if strings.HasPrefix(img.Source, registry) {
					pull.RegistryAuth = auth.Auth
				}
				if strings.HasPrefix(img.Target, registry) {
					push.RegistryAuth = auth.Auth
				}
			}
		}
		if err := processImage(cli, &img, &pull, &push); err != nil {
			return err
		}
	}
	return nil
}

func readAllToDiscard(r io.ReadCloser) error {
	defer func() {
		_ = r.Close()
	}()
	_, e := io.Copy(io.Discard, r)
	return e
}

func processImage(cli *client.Client, img *ImageConfig, pull *image.PullOptions, push *image.PushOptions) error {
	log.Printf("start to process image %s", img.Source)

	// Pull image
	reader, e := cli.ImagePull(context.Background(), img.Source, *pull)
	if e != nil {
		return fmt.Errorf("pull image %s failed: %w", img.Source, e)
	}
	if re := readAllToDiscard(reader); re != nil {
		return fmt.Errorf("error while pulling image %s: %w", img.Source, re)
	}
	log.Printf("pull image %s success", img.Source)

	// Tag image
	if e = cli.ImageTag(context.Background(), img.Source, img.Target); e != nil {
		return fmt.Errorf("tag image %s to %s failed: %w", img.Source, img.Target, e)
	}
	log.Printf("tag image %s to %s success", img.Source, img.Target)

	// Push image
	reader, e = cli.ImagePush(context.Background(), img.Target, *push)
	if e != nil {
		return fmt.Errorf("push image %s failed: %w", img.Target, e)
	}
	if re := readAllToDiscard(reader); re != nil {
		return fmt.Errorf("error while pushing image %s: %w", img.Target, re)
	}
	log.Printf("push image %s success", img.Target)

	return nil
}

func pruneUnusedImages(cli *client.Client) error {
	log.Println("Pruning unused and untagged images")

	images, err := cli.ImageList(context.Background(), image.ListOptions{
		All: true,
	})
	if err != nil {
		return fmt.Errorf("failed to list images: %w", err)
	}

	var spaceReclaimed int64
	var deletedCount int

	for _, img := range images {
		if len(img.RepoTags) > 0 {
			continue
		}
		if len(img.RepoTags) == 0 || (len(img.RepoTags) == 1 && strings.HasSuffix(img.RepoTags[0], ":<none>")) {
			_, e := cli.ImageRemove(context.Background(), img.ID, image.RemoveOptions{Force: true, PruneChildren: true})
			if e != nil {
				imageName := "<unnamed>"
				if len(img.RepoTags) > 0 {
					imageName = img.RepoTags[0]
				}
				log.Printf("Failed to remove image %s (ID: %s): %v", imageName, img.ID, e)
				continue
			}
			spaceReclaimed += img.Size
			deletedCount++
			log.Printf("Removed image: %s", img.ID)
		}
	}

	log.Printf("Pruned %d images, reclaimed space: %d bytes", deletedCount, spaceReclaimed)
	return nil
}
