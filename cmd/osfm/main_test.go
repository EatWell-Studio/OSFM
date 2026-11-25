package main

import (
	"testing"

	"github.com/spf13/cobra"

	"github.com/EatWell-Studio/OSFM/internal/config"
)

// fakeServe has the flag of PocketBase's serve command that useConfiguredListen changes, and records its value.
func fakeServe(got *string) *cobra.Command {
	var httpAddr string
	c := &cobra.Command{
		Use: "serve",
		RunE: func(*cobra.Command, []string) error {
			*got = httpAddr
			return nil
		},
	}
	c.PersistentFlags().StringVar(&httpAddr, "http", "", "")
	return c
}

func TestUseConfiguredListen(t *testing.T) {
	for _, tc := range []struct {
		name   string
		listen string // OSFM_LISTEN
		args   []string
		want   string
	}{
		{"default", "", nil, config.DefaultListen},
		{"from environment", "0.0.0.0:8090", nil, "0.0.0.0:8090"},
		{"explicit flag wins", "0.0.0.0:8090", []string{"--http", "127.0.0.1:9000"}, "127.0.0.1:9000"},
		{"domains keep PocketBase's default", "", []string{"example.org"}, ""},
		{"domains with environment", "0.0.0.0:8090", []string{"example.org"}, "0.0.0.0:8090"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := config.Parse(func(name string) (string, bool) {
				if name == config.EnvListen {
					return tc.listen, true
				}
				return "", false
			})
			if err != nil {
				t.Fatal(err)
			}
			var got string
			serve := fakeServe(&got)
			useConfiguredListen(serve, cfg)
			serve.SetArgs(tc.args)
			if err := serve.Execute(); err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("--http = %q, want %q", got, tc.want)
			}
		})
	}
}
