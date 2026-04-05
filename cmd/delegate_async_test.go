package cmd

import "testing"

func TestResolveEffectiveTimeout(t *testing.T) {
	tests := []struct {
		name          string
		flagTimeout   int
		configTimeout int
		minTimeout    int
		want          int
		wantRaised    bool
	}{
		{name: "use config when flag is zero", flagTimeout: 0, configTimeout: 300, minTimeout: 0, want: 300},
		{name: "use flag when lower than config", flagTimeout: 180, configTimeout: 300, minTimeout: 0, want: 180},
		{name: "cap to config when flag is higher", flagTimeout: 600, configTimeout: 300, minTimeout: 0, want: 300},
		{name: "use default when config is zero", flagTimeout: 0, configTimeout: 0, minTimeout: 0, want: 1800},
		{name: "use flag when config falls back to default", flagTimeout: 100, configTimeout: 0, minTimeout: 0, want: 100},
		{name: "treat negative flag as zero", flagTimeout: -1, configTimeout: 300, minTimeout: 0, want: 300},
		// min_timeout_secs tests
		{name: "min raises low flag timeout", flagTimeout: 60, configTimeout: 300, minTimeout: 120, want: 120, wantRaised: true},
		{name: "min raises low config timeout", flagTimeout: 0, configTimeout: 90, minTimeout: 120, want: 120, wantRaised: true},
		{name: "min does not lower higher timeout", flagTimeout: 0, configTimeout: 300, minTimeout: 120, want: 300},
		{name: "min zero means disabled", flagTimeout: 60, configTimeout: 300, minTimeout: 0, want: 60},
		{name: "min negative means disabled", flagTimeout: 60, configTimeout: 300, minTimeout: -1, want: 60},
		{name: "min equal to effective no raise", flagTimeout: 0, configTimeout: 120, minTimeout: 120, want: 120},
		{name: "config zero defaults to 1800 then floor", flagTimeout: 0, configTimeout: 0, minTimeout: 2000, want: 2000, wantRaised: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, raised := resolveEffectiveTimeout(tt.flagTimeout, tt.configTimeout, tt.minTimeout)
			if got != tt.want {
				t.Fatalf("resolveEffectiveTimeout(%d, %d, %d) = %d, want %d", tt.flagTimeout, tt.configTimeout, tt.minTimeout, got, tt.want)
			}
			if raised != tt.wantRaised {
				t.Fatalf("resolveEffectiveTimeout(%d, %d, %d) raised = %v, want %v", tt.flagTimeout, tt.configTimeout, tt.minTimeout, raised, tt.wantRaised)
			}
		})
	}
}
