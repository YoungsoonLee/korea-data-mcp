package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadEnvFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	os.WriteFile(path, []byte(`# keys
OPENDART_API_KEY=abc123
export NAVER_CLIENT_ID="id with spaces"
NAVER_CLIENT_SECRET='s3cr=t'

DATAGOKR_SERVICE_KEY = k+ey/==
`), 0o600)

	env, err := loadEnvFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"OPENDART_API_KEY":     "abc123",
		"NAVER_CLIENT_ID":      "id with spaces",
		"NAVER_CLIENT_SECRET":  "s3cr=t",
		"DATAGOKR_SERVICE_KEY": "k+ey/==",
	}
	if len(env) != len(want) {
		t.Fatalf("got %v", env)
	}
	for k, v := range want {
		if env[k] != v {
			t.Errorf("%s = %q, want %q", k, env[k], v)
		}
	}
}

func TestLoadEnvFileInvalidLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	os.WriteFile(path, []byte("OK=1\nnot a pair\n"), 0o600)
	if _, err := loadEnvFile(path); err == nil {
		t.Fatal("expected error")
	}
}

func TestEnvLookupPrefersProcessEnv(t *testing.T) {
	t.Setenv("MCPHUB_TEST_KEY", "from-shell")
	t.Setenv("MCPHUB_EMPTY", "")
	get := envLookup(map[string]string{"MCPHUB_TEST_KEY": "from-file", "MCPHUB_FILE_ONLY": "file", "MCPHUB_EMPTY": "file"})
	if get("MCPHUB_TEST_KEY") != "from-shell" || get("MCPHUB_FILE_ONLY") != "file" || get("MCPHUB_EMPTY") != "file" {
		t.Fatal("wrong precedence")
	}
}
