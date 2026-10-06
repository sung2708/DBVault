package cli

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/spf13/cobra"
	"github.com/sung2708/DBVault/internal/app"
	"github.com/sung2708/DBVault/internal/storage/providers"
)

func (o *options) metricsCommand() *cobra.Command {
	var listen string
	cmd := &cobra.Command{Use: "metrics", GroupID: "operations", Short: "Print Prometheus metrics or serve a metrics endpoint", Args: noPositionalArgs,
		Long:    "Read backup manifests and durable operation records without opening archives.\nPrint Prometheus text, or use --listen 127.0.0.1:9090 to serve /metrics until Ctrl+C.\nCounters cover recorded operations since this feature was introduced.\nThe HTTP endpoint has no authentication; keep it on a trusted network.",
		Example: "  dbvault metrics\n  dbvault metrics --listen 127.0.0.1:9090", RunE: func(c *cobra.Command, _ []string) (resultErr error) {
			defer func() { resultErr = o.redactor.Error(resultErr) }()
			if jsonMode(c) {
				return fmt.Errorf("metrics uses Prometheus text; omit --json/--output json")
			}
			cfg, err := o.load(c)
			if err != nil {
				return err
			}
			store, closeStore, err := providers.Open(c.Context(), cfg.Storage)
			if err != nil {
				return err
			}
			defer closeStore()
			svc := &app.Service{Config: cfg, Store: store}
			if listen == "" {
				ctx, cancel := context.WithTimeout(c.Context(), 30*time.Second)
				defer cancel()
				text, err := svc.Metrics(ctx)
				if err != nil {
					return err
				}
				_, err = fmt.Fprint(c.OutOrStdout(), text)
				return err
			}
			listener, err := net.Listen("tcp", listen)
			if err != nil {
				return err
			}
			defer listener.Close()
			mux := http.NewServeMux()
			mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					w.WriteHeader(http.StatusMethodNotAllowed)
					return
				}
				ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
				defer cancel()
				text, err := svc.Metrics(ctx)
				if err != nil {
					http.Error(w, "metrics unavailable", http.StatusServiceUnavailable)
					return
				}
				w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
				fmt.Fprint(w, text)
			})
			server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second, WriteTimeout: 35 * time.Second, IdleTimeout: time.Minute}
			done := make(chan error, 1)
			go func() { done <- server.Serve(listener) }()
			select {
			case err := <-done:
				if errors.Is(err, http.ErrServerClosed) {
					return nil
				}
				return err
			case <-c.Context().Done():
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				err := server.Shutdown(ctx)
				if err != nil {
					server.Close()
				}
				return err
			}
		}}
	cmd.Flags().StringVar(&listen, "listen", "", "Serve /metrics at this TCP address (e.g. 127.0.0.1:9090)")
	return cmd
}
