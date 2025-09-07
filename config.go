package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"log"
	"os"
	"path"
	"strings"

	"github.com/docker/docker/api/types/registry"
	"github.com/go-sphere/confstore"
	"github.com/go-sphere/confstore/codec"
	"github.com/go-sphere/confstore/provider"
	"github.com/go-sphere/confstore/provider/file"
	"github.com/go-sphere/confstore/provider/http"
)

type RegistryAuth struct {
	Auth     string `json:"auth"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type ImageConfig struct {
	Source string `json:"source"`
	Target string `json:"target"`
}

type ImageWebhook struct {
	URL    string `json:"url"`
	Method string `json:"method"`
}

type DockerConfig struct {
	Auths map[string]RegistryAuth `json:"auths"`
}

type Config struct {
	Images       []ImageConfig           `json:"images"`
	Auths        map[string]RegistryAuth `json:"auths"`
	Duration     int                     `json:"duration"`
	DisablePrune bool                    `json:"disable_prune"`
}

func newProvider(path string) (provider.Provider, error) {
	if http.IsRemoteURL(path) {
		return http.New(path, http.WithTimeout(10)), nil
	}
	if file.IsLocalPath(path) {
		return file.New(path, file.WithExpandEnv()), nil
	}
	return nil, errors.New("unsupported config path")
}

func loadConfig(path string) (*Config, error) {
	prov, err := provider.Selector(
		path,
		provider.If(file.IsLocalPath, func(s string) provider.Provider {
			return file.New(path, file.WithExpandEnv())
		}),
		provider.If(http.IsRemoteURL, func(s string) provider.Provider {
			return http.New(path, http.WithTimeout(10))
		}),
	)
	if err != nil {
		return nil, err
	}
	config, err := confstore.Load[Config](prov, codec.JsonCodec())
	if err != nil {
		return nil, err
	}
	if len(config.Auths) == 0 {
		log.Printf("No auths found in config, loading default auth")
		config.Auths = loadDefaultAuth()
	} else {
		log.Printf("Found auths in config: %+v", config.Auths)
		auths := make(map[string]RegistryAuth)
		for i, auth := range config.Auths {
			if auth.Auth == "" {
				authConfig := registry.AuthConfig{
					Username: auth.Username,
					Password: auth.Password,
				}
				authStr, e := registry.EncodeAuthConfig(authConfig)
				if e != nil {
					log.Printf("Failed to encode auth for %s: %v", auth.Username, e)
					continue
				}
				auths[i] = RegistryAuth{
					Auth: authStr,
				}
				log.Printf("Encoded auth for %s", auth.Auth)
			} else {
				auths[i] = auth
			}
		}
		config.Auths = auths
	}

	return config, nil
}

func loadDefaultAuth() map[string]RegistryAuth {

	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}

	conf := path.Join(home, ".docker", "config.json")
	log.Printf("Looking for auth in %s", conf)
	if _, e := os.Stat(conf); e != nil {
		log.Printf("No auth found in %s", conf)
		return nil
	}

	data, err := os.ReadFile(conf)
	if err != nil {
		log.Printf("Failed to read %s: %v", conf, err)
		return nil
	}

	var dockerConfig DockerConfig
	if e := json.Unmarshal(data, &dockerConfig); e != nil {
		log.Printf("Failed to parse %s: %v", conf, e)
		return nil
	}
	auths := make(map[string]RegistryAuth)
	for i, auth := range dockerConfig.Auths {
		decodedAuth, e := base64.URLEncoding.DecodeString(auth.Auth)
		if e != nil {
			continue
		}
		credentials := strings.SplitN(string(decodedAuth), ":", 2)
		if len(credentials) != 2 {
			continue
		}
		authConfig := registry.AuthConfig{
			Username: credentials[0],
			Password: credentials[1],
		}
		authStr, e := registry.EncodeAuthConfig(authConfig)
		if e != nil {
			continue
		}
		auths[i] = RegistryAuth{
			Auth: authStr,
		}
	}
	return auths
}
