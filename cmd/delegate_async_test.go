package cmd

import "testing"

func TestResolveEffectiveTimeout(t *testing.T) {
	tests := []struct {
		name          string
		flagTimeout   int
		configTimeout int
		want          int
	}{
		{name: "use config when flag is zero", flagTimeout: 0, configTimeout: 300, want: 300},
		{name: "use flag when lower than config", flagTimeout: 180, configTimeout: 300, want: 180},
		{name: "cap to config when flag is higher", flagTimeout: 600, configTimeout: 300, want: 300},
		{name: "use default when config is zero", flagTimeout: 0, configTimeout: 0, want: 1800},
		{name: "use flag when config falls back to default", flagTimeout: 100, configTimeout: 0, want: 100},
		{name: "treat negative flag as zero", flagTimeout: -1, configTimeout: 300, want: 300},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveEffectiveTimeout(tt.flagTimeout, tt.configTimeout); got != tt.want {
				t.Fatalf("resolveEffectiveTimeout(%d, %d) = %d, want %d", tt.flagTimeout, tt.configTimeout, got, tt.want)
			}
		})
	}
}
