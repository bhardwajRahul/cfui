package main

import (
	"errors"
	"io/fs"
	"sort"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

func TestEmbeddedLocaleKeyParity(t *testing.T) {
	keysByLang := map[string]map[string]bool{}
	for _, lang := range []string{"en", "zh", "ja"} {
		keys, err := embeddedLocaleKeys(lang)
		if err != nil {
			t.Fatalf("locale %s: %v", lang, err)
		}
		keysByLang[lang] = keys
	}

	base := keysByLang["en"]
	for _, lang := range []string{"zh", "ja"} {
		if missing := missingLocaleKeys(base, keysByLang[lang]); len(missing) > 0 {
			t.Fatalf("%s locale missing keys: %v", lang, missing)
		}
		if extra := missingLocaleKeys(keysByLang[lang], base); len(extra) > 0 {
			t.Fatalf("%s locale extra keys: %v", lang, extra)
		}
	}
}

func embeddedLocaleKeys(lang string) (map[string]bool, error) {
	keys := map[string]bool{}
	loadFile := func(path string) error {
		data, err := fs.ReadFile(locales, path)
		if err != nil {
			return err
		}
		var translations map[string]map[string]string
		if err := toml.Unmarshal(data, &translations); err != nil {
			return err
		}
		for key := range translations {
			keys[key] = true
		}
		return nil
	}
	if err := loadFile("locales/" + lang + ".toml"); err != nil {
		return nil, err
	}
	entries, err := fs.ReadDir(locales, "locales/"+lang)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return keys, nil
		}
		return nil, err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".toml") {
			continue
		}
		if err := loadFile("locales/" + lang + "/" + entry.Name()); err != nil {
			return nil, err
		}
	}
	return keys, nil
}

func missingLocaleKeys(want, got map[string]bool) []string {
	var missing []string
	for key := range want {
		if !got[key] {
			missing = append(missing, key)
		}
	}
	sort.Strings(missing)
	return missing
}
