package config_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/JtheGunner/doc-drift-checker/internal/config"
)

func TestStarterParsesToDefaults(t *testing.T) {
	cfg, err := config.Parse(config.Starter(), config.FileName)
	if err != nil {
		t.Fatalf("Parse(Starter()) = %v", err)
	}
	if want := config.Defaults(); !reflect.DeepEqual(cfg, want) {
		t.Errorf("Starter config = %+v, want defaults %+v", cfg, want)
	}
}

func TestStarterIsDeterministicAndCommented(t *testing.T) {
	first, second := string(config.Starter()), string(config.Starter())
	if first != second {
		t.Error("Starter() is not deterministic")
	}
	if !strings.HasSuffix(first, "\n") || strings.Contains(first, "\r") {
		t.Error("Starter() must be LF-terminated without CR")
	}
	for _, key := range []string{"docs:", "ignore:", "mappings:", "mode:", "default: diff", "severity:", "fail-on: low"} {
		if !strings.Contains(first, key) {
			t.Errorf("Starter() lacks %q", key)
		}
	}
	for _, glob := range config.DefaultIgnore() {
		if !strings.Contains(first, `"`+glob+`"`) {
			t.Errorf("Starter() lacks default ignore glob %q", glob)
		}
	}
}

func TestStarterMappingsExampleIsValidWhenUncommented(t *testing.T) {
	var uncommented []string
	in := false
	for _, line := range strings.Split(string(config.Starter()), "\n") {
		if strings.HasPrefix(line, "# mappings:") {
			in = true
		}
		if in {
			if !strings.HasPrefix(line, "# ") {
				break
			}
			uncommented = append(uncommented, strings.TrimPrefix(line, "# "))
		}
	}
	if len(uncommented) == 0 {
		t.Fatal("no commented mappings example found")
	}
	cfg, err := config.Parse([]byte(strings.Join(uncommented, "\n")), "example")
	if err != nil {
		t.Fatalf("mappings example is invalid: %v", err)
	}
	if len(cfg.Mappings) == 0 {
		t.Error("mappings example yields no mappings")
	}
}
