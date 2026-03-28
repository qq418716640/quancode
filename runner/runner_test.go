package runner

import (
	"reflect"
	"testing"
)

func TestMergeEnvOverridesExistingKeysCaseInsensitively(t *testing.T) {
	base := []string{
		"PATH=/usr/bin",
		"HTTP_PROXY=http://old-proxy",
		"LANG=en_US.UTF-8",
	}
	extra := map[string]string{
		"http_proxy": "http://new-proxy",
		"NO_PROXY":   "localhost,127.0.0.1",
	}

	got := MergeEnv(base, extra)

	want := []string{
		"PATH=/usr/bin",
		"http_proxy=http://new-proxy",
		"LANG=en_US.UTF-8",
		"NO_PROXY=localhost,127.0.0.1",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("MergeEnv mismatch\nwant: %#v\ngot:  %#v", want, got)
	}
}

func TestMergeEnvReturnsBaseWhenExtraEmpty(t *testing.T) {
	base := []string{"PATH=/usr/bin"}

	got := MergeEnv(base, nil)

	if !reflect.DeepEqual(got, base) {
		t.Fatalf("expected base env unchanged, got %#v", got)
	}
}
