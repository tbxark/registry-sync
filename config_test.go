package main

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/registry"
)

func TestLoadConfigDoesNotLogRegistryCredentials(t *testing.T) {
	const (
		username = "dummy-registry-user"
		password = "dummy-registry-password"
		token    = "dummy-pre-encoded-registry-auth"
	)
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"auths":{
		"registry.example":{"username":"`+username+`","password":"`+password+`"},
		"mirror.example":{"auth":"`+token+`"}
	}}`), 0600); err != nil {
		t.Fatal(err)
	}

	var logs bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(previous) })

	config, err := loadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	encoded := config.Auths["registry.example"].Auth
	decoded, err := registry.DecodeAuthConfig(encoded)
	if err != nil || decoded.Username != username || decoded.Password != password {
		t.Fatal("username/password entry was not encoded correctly")
	}
	if config.Auths["mirror.example"].Auth != token {
		t.Fatal("pre-encoded auth entry was not preserved")
	}

	for _, entry := range []struct{ name, value string }{
		{"username", username},
		{"password", password},
		{"pre-encoded auth", token},
		{"newly encoded auth", encoded},
	} {
		if strings.Contains(logs.String(), entry.value) {
			t.Errorf("config loader logged %s", entry.name)
		}
	}
}
