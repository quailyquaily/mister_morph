package integration

import "testing"

func TestNewUsesFeaturesExactlyAsConfigured(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
		want Features
	}{
		{
			name: "zero config keeps every feature disabled",
			cfg:  Config{},
			want: Features{},
		},
		{
			name: "default config enables default features",
			cfg:  DefaultConfig(),
			want: DefaultFeatures(),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runtime := New(tt.cfg)
			if runtime.features != tt.want {
				t.Fatalf("features = %#v, want %#v", runtime.features, tt.want)
			}
		})
	}
}
