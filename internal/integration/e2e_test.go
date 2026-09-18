//go:build integration

// Package integration runs end-to-end tests with real Postgres via
// testcontainers + httptest. Run with:
//
//	go test -tags=integration ./internal/integration/...
//
// These tests are excluded from the default `go test ./...` (the //go:build
// integration tag prevents compilation without it).
package integration

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/jmoiron/sqlx"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/sgtour/webhook-relay/internal/application"
	"github.com/sgtour/webhook-relay/internal/domain"
	infrahttp "github.com/sgtour/webhook-relay/internal/infra/http"
	pgadapter "github.com/sgtour/webhook-relay/internal/infra/postgres"
	"github.com/sgtour/webhook-relay/internal/infra/webhook"
	"github.com/sgtour/webhook-relay/internal/infra/worker"
)

// --- Test helpers ---
