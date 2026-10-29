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
	t.Setenv("KOREADATA_TEST_KEY", "from-shell")
	t.Setenv("KOREADATA_EMPTY", "")
	get := envLookup(map[string]string{"KOREADATA_TEST_KEY": "from-file", "KOREADATA_FILE_ONLY": "file", "KOREADATA_EMPTY": "file"})
	if get("KOREADATA_TEST_KEY") != "from-shell" || get("KOREADATA_FILE_ONLY") != "file" || get("KOREADATA_EMPTY") != "file" {
		t.Fatal("wrong precedence")
	}
}

// An optional MCPB setting the user skipped may arrive as the literal
// ${user_config.x}; it must not enable a provider with a bogus key.
func TestEnvLookupUnexpanded(t *testing.T) {
	t.Setenv("KOREADATA_SKIPPED", "${user_config.lawgokr_oc}")
	t.Setenv("KOREADATA_SHADOWED", "${user_config.opendart_api_key}")
	get := envLookup(map[string]string{"KOREADATA_SHADOWED": "file"})
	if get("KOREADATA_SKIPPED") != "" || get("KOREADATA_SHADOWED") != "file" {
		t.Fatalf("got %q, %q", get("KOREADATA_SKIPPED"), get("KOREADATA_SHADOWED"))
	}
}
