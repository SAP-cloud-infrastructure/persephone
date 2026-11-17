// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Project Persephone contributors
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/go-logr/logr"
	"github.com/sapcc/go-api-declarations/bininfo"
	auditv1 "k8s.io/apiserver/pkg/apis/audit/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"

	secretsutils "github.com/gardener/gardener/pkg/utils/secrets"

	"github.com/sap-cloud-infrastructure/persephone/cmd/persephone-auditlog-receiver/options"
	"github.com/sap-cloud-infrastructure/persephone/internal/receiver"
)

func main() {
	bininfo.HandleVersionArgument()
	opts := &options.Options{}
	opts.AddFlags()
	flag.Parse()

	log := zap.New(zap.UseFlagOptions(&opts.Zap)).WithName("auditlog-receiver")
	klog.SetLogger(log)

	if err := run(signals.SetupSignalHandler(), log, opts); err != nil {
		log.Error(err, "fatal error")
		os.Exit(1)
	}
}

func run(ctx context.Context, logger logr.Logger, opts *options.Options) error {
	if err := opts.Validate(); err != nil {
		return fmt.Errorf("invalid options: %w", err)
	}

	accessKey, secretKey, err := options.S3Credentials()
	if err != nil {
		return fmt.Errorf("s3 credentials: %w", err)
	}

	if opts.S3Endpoint == "" {
		return errors.New("s3-endpoint must be set")
	}

	var objectLock *receiver.ObjectLockCfg
	if opts.ObjectLockEnabled {
		objectLock = &receiver.ObjectLockCfg{
			RetentionDays: int32(opts.ObjectLockRetentionDays), //nolint:gosec
			Mode:          opts.ObjectLockMode,
		}
	}

	writer, err := receiver.NewS3Writer(ctx, logger.WithName("s3"),
		accessKey, secretKey, opts.S3Endpoint, opts.Bucket, opts.Prefix, objectLock)
	if err != nil {
		return fmt.Errorf("s3 writer: %w", err)
	}

	flushInterval := time.Duration(opts.FlushIntervalSeconds) * time.Second
	buf := receiver.NewBuffer(flushInterval, opts.MaxEvents, func(events []auditv1.Event) {
		if err := writer.Write(ctx, events); err != nil {
			logger.Error(err, "failed to write events to s3")
		}
	})
	go buf.Start(ctx)

	mux := http.NewServeMux()
	mux.Handle(receiver.AuditPath, &receiver.Handler{
		Logger: logger.WithName("handler"),
		Buffer: buf,
	})
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	tlsCfg, err := buildTLSConfig(opts.CertDir)
	if err != nil {
		return fmt.Errorf("build tls config: %w", err)
	}

	srv := &http.Server{
		Addr:      opts.Addr,
		Handler:   mux,
		TLSConfig: tlsCfg,
	}

	logger.Info("starting auditlog receiver", "addr", opts.Addr)
	errCh := make(chan error, 1)
	go func() {
		certFile := filepath.Join(opts.CertDir, secretsutils.DataKeyCertificate)
		keyFile := filepath.Join(opts.CertDir, secretsutils.DataKeyPrivateKey)
		errCh <- srv.ListenAndServeTLS(certFile, keyFile)
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}

func buildTLSConfig(certDir string) (*tls.Config, error) {
	caPEM, err := os.ReadFile(filepath.Join(certDir, secretsutils.DataKeyCertificateCA))
	if err != nil {
		return nil, fmt.Errorf("read CA: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, errors.New("failed to parse CA certificate")
	}
	return &tls.Config{
		ClientAuth: tls.RequireAndVerifyClientCert,
		ClientCAs:  pool,
		MinVersion: tls.VersionTLS13,
	}, nil
}
