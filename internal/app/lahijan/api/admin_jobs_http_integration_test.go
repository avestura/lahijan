// admin_jobs_http_integration_test.go exercises the /api/v1/admin/jobs* HTTP
// surface end to end against a real Postgres (testcontainers) plus a real
// River client. It is the WS-09 DoD requirement that "DLQ jobs can be
// retried via the admin API", "admin API requires platform.admin", and
// "every privileged admin action emits an audit event".
//
// Audit events are verified by querying the audit_log table directly rather
// than mocking the emitter — keeps the test setup small and exercises the
// full DB-backed emitter path end to end.

//go:build integration

package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/avestura/lahijan/internal/app/lahijan/api"
	"github.com/avestura/lahijan/internal/app/lahijan/api/middleware"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/audit"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/email"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/password"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/pat"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/rbac"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/secrets"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/session"
	"github.com/avestura/lahijan/internal/app/lahijan/database/testutil"
	"github.com/avestura/lahijan/internal/app/lahijan/jobs"
	notifyemail "github.com/avestura/lahijan/internal/app/lahijan/notify/email"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// fastRetryPolicy100ms schedules every retry 100ms out so the DLQ tests
// don't have to wait for River's default attempt^4-second backoff.
type fastRetryPolicy100ms struct{}

func (fastRetryPolicy100ms) NextRetry(*rivertype.JobRow) time.Time {
	return time.Now().UTC().Add(100 * time.Millisecond)
}

// adminTestArgs is a tiny kind registered on a unique queue per test so the
// admin-jobs HTTP tests do not collide with each other.
type adminTestArgs struct{ N int }

func (adminTestArgs) Kind() string { return "test.admin_jobs" }

type adminTestWorker struct {
	river.WorkerDefaults[adminTestArgs]
}

func (adminTestWorker) Work(_ context.Context, _ *river.Job[adminTestArgs]) error {
	return nil
}

// retryFailArgs / retryFailWorker — a kind that always fails, used to drive
// a job to DLQ so the retry endpoint has something to retry.
type retryFailArgs struct{ Reason string }

func (retryFailArgs) Kind() string { return "test.retry_fail" }

type retryFailWorker struct {
	river.WorkerDefaults[retryFailArgs]
}

func (retryFailWorker) Work(_ context.Context, job *river.Job[retryFailArgs]) error {
	return fmt.Errorf("retry_fail: %s", job.Args.Reason)
}

// jobsTestApp bundles a testApp with a running jobs client + supervisor.
type jobsTestApp struct {
	ta   *testApp
	jobs *jobs.Client
	// queue is the per-test unique queue every worker in this app's
	// registry is registered on. Tests pass it as InsertOpts.Queue.
	queue string
}

// newJobsTestApp builds a fresh testApp + jobs client. The two are wired
// together via api.ServerDeps.Jobs so the admin jobs endpoints reach the
// River client. All other deps are inherited from newTestApp's pattern.
func newJobsTestApp(t *testing.T) *jobsTestApp {
	t.Helper()
	repos := testutil.Repos()
	signer := secrets.NewSigner("http-test-signing-key")
	hasher := password.NewHasher(4, 1, 1, 8, 16)
	cookies := api.CookieConfig{
		SessionName: "lahijan_session", RefreshName: "lahijan_refresh",
		Path: "/", SameSite: "lax", SessionMaxAge: 3600, RefreshMaxAge: 3600,
	}
	mailer := email.New(
		repos.Users, repos.EmailTokens, hasher, signer,
		notifyemail.NoopSender{}, audit.NoopEmitter{},
		email.Config{
			VerifyTTL: time.Hour, ResetTTL: time.Hour, EmailChangeTTL: time.Hour,
			TokenByteLen: 32, AppBaseURL: "https://app.test",
		},
	)
	sessionSvc := session.New(
		repos.Users, repos.Sessions, repos.Tokens, hasher, signer,
		mailer, audit.NoopEmitter{},
		session.Config{
			SessionLifetime: time.Hour, RefreshLifetime: time.Hour,
			TokenByteLen: 32, MinPasswordLen: 12,
		},
	)
	patSvc := pat.New(
		repos.Tokens, signer, audit.NoopEmitter{},
		pat.Config{Prefix: "lah_pat_", ByteLen: 32},
	)

	// Jobs: unique queue + both test kinds registered, fast retry policy.
	queue := fmt.Sprintf("admin_jobs_%s", uuid.NewString()[:8])
	registry := jobs.NewRegistry()
	jobs.Register(registry, adminTestArgs{}, adminTestWorker{}, jobs.KindSpec{
		Kind: (adminTestArgs{}).Kind(), Queue: queue,
	})
	jobs.Register(registry, retryFailArgs{}, retryFailWorker{}, jobs.KindSpec{
		Kind: (retryFailArgs{}).Kind(), Queue: queue,
	})
	cfg := jobs.DefaultConfig()
	cfg.Logger = slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
	cfg.Queues = map[string]int{queue: 5}
	cfg.RetryPolicy = fastRetryPolicy100ms{}
	jobsClient, err := jobs.NewClient(testutil.Pool(), registry, cfg)
	require.NoError(t, err)
	sup, err := jobs.NewSupervisor(jobsClient, 5*time.Second)
	require.NoError(t, err)
	// Use context.Background() — River ties the client's lifetime to this
	// context. A timeout here would shut the workers down after the
	// timeout; we want them running for the whole test, drained by Stop.
	require.NoError(t, sup.Start(context.Background()))
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer stopCancel()
		_ = sup.Stop(stopCtx)
	})

	server := api.NewServer(api.ServerDeps{
		Users:        repos.Users,
		Sessions:     repos.Sessions,
		SessionSvc:   sessionSvc,
		PATSvc:       patSvc,
		EmailSvc:     mailer,
		Signer:       signer,
		Cookies:      cookies,
		Audit:        repos.AuditLog,
		AuditEmitter: audit.NewDBEmitter(repos.AuditLog),
		Jobs:         jobsClient,
	})
	policy := middleware.NewPolicyResolver(rbac.NewEvaluator(repos.Memberships))
	app := fiber.New()
	middleware.Apply(app, middleware.Options{
		Tenant: middleware.TenantWithResolver(middleware.TenantResolver{Tenants: repos.Tenants}),
		Auth: middleware.AuthWithResolver(middleware.AuthResolver{
			Cookies: middleware.CookieConfig{
				SessionName: cookies.SessionName,
				RefreshName: cookies.RefreshName,
			},
			Signer:       signer,
			SessionsRepo: repos.Sessions,
			PATService:   patSvc,
		}),
	})
	api.RegisterRoutes(app, server, policy)
	ta := &testApp{app: app, cookies: cookies, repos: repos}
	return &jobsTestApp{ta: ta, jobs: jobsClient, queue: queue}
}

// registerPlatformAdmin seeds RBAC and grants the user the platform.admin
// role in a tenant so the admin jobs endpoints resolve to "allowed".
func registerPlatformAdmin(t *testing.T, ta *testApp) (uuid.UUID, string, string) {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, rbac.SeedOnce(ctx, testutil.Repos()))
	pool := testutil.Pool()
	tenant := testutil.NewTenant(ctx, t, pool)
	role, err := testutil.Repos().RBAC.GetRoleBySlug(ctx, rbac.RolePlatformAdmin)
	require.NoError(t, err)

	addr := "admin+" + uuid.NewString()[:12] + "@example.test"
	status, _, sc := doJSON(t, ta, "POST", "/api/v1/auth/register",
		map[string]any{"email": addr, "password": strongPw, "locale": "en"}, "")
	require.Equalf(t, 201, status, "register must succeed (got %d)", status)
	sess := extractCookie(sc, "lahijan_session")
	require.NotEmpty(t, sess, "register must set session cookie")

	user, err := ta.repos.Users.GetByEmail(ctx, addr)
	require.NoError(t, err)
	testutil.NewMembership(ctx, t, pool, tenant.ID, user.ID, &role.ID)
	return user.ID, sess, tenant.ID.String()
}

// countAuditActions returns how many rows in audit_log match the given
// action since the given timestamp. Tests use this to assert that an admin
// action emitted exactly one audit event.
func countAuditActions(t *testing.T, action string, since time.Time) int {
	t.Helper()
	ctx := context.Background()
	var n int
	err := testutil.Pool().QueryRow(
		ctx,
		"SELECT COUNT(*) FROM audit_log WHERE action = $1 AND created_at >= $2",
		action, since,
	).Scan(&n)
	require.NoError(t, err, "count audit_log")
	return n
}

// TestListAdminJobs_RequiresPlatformAdmin proves a tenant viewer (not a
// platform admin) is rejected with 403 from the admin jobs gate. This is
// the WS-09 DoD item "admin API requires platform.admin permission".
func TestListAdminJobs_RequiresPlatformAdmin(t *testing.T) {
	jta := newJobsTestApp(t)
	_, sess, tidStr := registerAndLogin(t, jta.ta, rbac.RoleTenantViewer)

	req := httptest.NewRequest("GET", "/api/v1/admin/jobs", nil)
	req.Header.Set("Cookie", "lahijan_session="+sess)
	req.Header.Set(middleware.HeaderTenantID, tidStr)
	resp, err := jta.ta.app.Test(req, -1)
	require.NoError(t, err)
	assert.Equal(t, 403, resp.StatusCode, "tenant viewer should not reach admin jobs")
}

// TestListAdminJobs_ReturnsJobs proves a platform admin can list jobs.
func TestListAdminJobs_ReturnsJobs(t *testing.T) {
	jta := newJobsTestApp(t)
	_, sess, tidStr := registerPlatformAdmin(t, jta.ta)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := jta.jobs.Insert(ctx, adminTestArgs{N: 1}, &river.InsertOpts{Queue: jta.queue})
	require.NoError(t, err)

	req := httptest.NewRequest("GET", "/api/v1/admin/jobs", nil)
	req.Header.Set("Cookie", "lahijan_session="+sess)
	req.Header.Set(middleware.HeaderTenantID, tidStr)
	resp, err := jta.ta.app.Test(req, -1)
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	var page struct {
		Items []map[string]any `json:"items"`
		Total int64            `json:"total"`
	}
	require.NoError(t, json.Unmarshal(body, &page))
	assert.GreaterOrEqual(t, page.Total, int64(1))
	require.NotEmpty(t, page.Items)
}

// TestRetryAdminJob_OnDiscardedJob proves a DLQ'd job can be retried via the
// admin API AND that the action emits an audit row. This is the WS-09 DoD
// item "DLQ jobs can be retried via the admin API" + "every privileged admin
// action emits an audit event".
func TestRetryAdminJob_OnDiscardedJob(t *testing.T) {
	jta := newJobsTestApp(t)
	_, sess, tidStr := registerPlatformAdmin(t, jta.ta)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	before := time.Now().UTC()

	res, err := jta.jobs.Insert(ctx, retryFailArgs{Reason: "for DLQ"}, &river.InsertOpts{
		MaxAttempts: 2,
		Queue:       jta.queue,
	})
	require.NoError(t, err)
	// Wait for the job to land in DLQ.
	require.Eventually(t, func() bool {
		row, err := jta.jobs.JobGet(ctx, res.Job.ID)
		if err != nil {
			return false
		}
		return row.State == "discarded"
	}, 25*time.Second, 250*time.Millisecond, "job should reach discarded state")

	// Hit the retry endpoint.
	req := httptest.NewRequest("POST",
		fmt.Sprintf("/api/v1/admin/jobs/%d/retry", res.Job.ID), nil)
	req.Header.Set("Cookie", "lahijan_session="+sess)
	req.Header.Set(middleware.HeaderTenantID, tidStr)
	resp, err := jta.ta.app.Test(req, -1)
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode, "retry should succeed on discarded job")

	row, err := jta.jobs.JobGet(ctx, res.Job.ID)
	require.NoError(t, err)
	assert.NotEqual(t, "discarded", string(row.State), "job should no longer be discarded after retry")

	n := countAuditActions(t, audit.ActionPlatformJobRetry, before)
	assert.GreaterOrEqual(t, n, 1, "retry should emit audit event")
}

// TestCancelAdminJob_OnQueuedJob proves the cancel endpoint works on a
// non-terminal job and emits an audit row.
func TestCancelAdminJob_OnQueuedJob(t *testing.T) {
	jta := newJobsTestApp(t)
	_, sess, tidStr := registerPlatformAdmin(t, jta.ta)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	before := time.Now().UTC()

	res, err := jta.jobs.Insert(ctx, adminTestArgs{N: 1}, &river.InsertOpts{
		Queue:       jta.queue,
		ScheduledAt: time.Now().Add(1 * time.Hour), // delay so it stays available
	})
	require.NoError(t, err)

	req := httptest.NewRequest("POST",
		fmt.Sprintf("/api/v1/admin/jobs/%d/cancel", res.Job.ID), nil)
	req.Header.Set("Cookie", "lahijan_session="+sess)
	req.Header.Set(middleware.HeaderTenantID, tidStr)
	resp, err := jta.ta.app.Test(req, -1)
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)

	row, err := jta.jobs.JobGet(ctx, res.Job.ID)
	require.NoError(t, err)
	assert.Equal(t, "cancelled", string(row.State))

	n := countAuditActions(t, audit.ActionPlatformJobCancel, before)
	assert.GreaterOrEqual(t, n, 1, "cancel should emit audit event")
}
