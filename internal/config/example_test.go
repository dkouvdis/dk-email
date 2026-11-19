package config

import (
	"os"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"
)

const exampleConfigPath = "../../config.example.toml"

func TestExampleConfigLoads(t *testing.T) {
	cfg, err := Load(exampleConfigPath)
	if err != nil {
		t.Fatalf("Load(%s) error = %v", exampleConfigPath, err)
	}

	if len(cfg.Accounts) != 1 {
		t.Fatalf("Accounts = %d, want 1", len(cfg.Accounts))
	}
	a := cfg.Accounts[0]
	if a.Name != "Personal" {
		t.Errorf("account name = %q, want %q", a.Name, "Personal")
	}
	if a.SMTPHost == "" {
		t.Error("account has no smtp_host, want sending enabled")
	}
	want := Default()
	if cfg.UI != want.UI {
		t.Errorf("UI = %+v, want %+v", cfg.UI, want.UI)
	}
	if cfg.UI.RefreshInterval.Duration != 120*time.Second {
		t.Errorf("refresh_interval = %s, want 2m0s", cfg.UI.RefreshInterval)
	}
	if cfg.Notify != want.Notify {
		t.Errorf("Notify = %+v, want %+v", cfg.Notify, want.Notify)
	}
	if cfg.Colors != (Colors{}) {
		t.Errorf("Colors = %+v, want none set", cfg.Colors)
	}
}

func TestExampleConfigMentionsEveryKey(t *testing.T) {
	data, err := os.ReadFile(exampleConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)

	var missing []string
	for _, key := range tomlKeys(UI{}, Notify{}, Colors{}, Account{}, Alias{}) {
		re := regexp.MustCompile(`(?m)^\s*#?\s*` + regexp.QuoteMeta(key) + `\s*=`)
		if re.MatchString(text) {
			continue
		}
		// Alias fields may appear only inside the aliases inline table.
		if (key == "email" || key == "name") && regexp.MustCompile(`\b`+key+`\s*=`).MatchString(text) {
			continue
		}
		missing = append(missing, key)
	}
	if len(missing) > 0 {
		t.Errorf("config.example.toml does not mention these keys: %s", strings.Join(missing, ", "))
	}
}

// tomlKeys returns the sorted, de-duplicated toml key names of the struct
// values' fields, skipping toml:"-".
func tomlKeys(structs ...any) []string {
	seen := map[string]bool{}
	for _, s := range structs {
		rt := reflect.TypeOf(s)
		for i := 0; i < rt.NumField(); i++ {
			name, _, _ := strings.Cut(rt.Field(i).Tag.Get("toml"), ",")
			if name != "" && name != "-" {
				seen[name] = true
			}
		}
	}
	keys := make([]string, 0, len(seen))
	for k := range seen {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
