package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/redis/go-redis/v9"
	"github.com/gantry-project/gantry/internal/models"
	"github.com/gantry-project/gantry/internal/queue"
)

type fakeQueue struct {
	jobs []queue.BuildJob
}

func (f *fakeQueue) Enqueue(_ context.Context, job queue.BuildJob) error {
	f.jobs = append(f.jobs, job)
	return nil
}

func (f *fakeQueue) SubscribeLogs(_ context.Context, _ string) *redis.PubSub {
	return nil
}

func TestServerDeployListAndGet(t *testing.T) {
	fq := &fakeQueue{}
	store := models.NewDeploymentStore()
	srv := NewServer(fq, store)

	// 1. Health check
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/health", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on /api/health, got %d", rec.Code)
	}

	// 2. Deploy returns 202 Accepted
	body, _ := json.Marshal(DeployRequest{AppName: "web", SrcPath: "/tmp/web"})
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/deploy", bytes.NewReader(body)))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202 on /api/deploy, got %d: %s", rec.Code, rec.Body.String())
	}
	if len(fq.jobs) != 1 || fq.jobs[0].AppName != "web" {
		t.Fatalf("expected 1 enqueued job for web, got %+v", fq.jobs)
	}

	var depResp DeployResponse
	if err := json.NewDecoder(rec.Body).Decode(&depResp); err != nil {
		t.Fatal(err)
	}

	// 3. List deployments
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/deployments", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on /api/deployments, got %d", rec.Code)
	}
	var list []*models.Deployment
	if err := json.NewDecoder(rec.Body).Decode(&list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != depResp.DeploymentID {
		t.Fatalf("unexpected deployment list: %+v", list)
	}

	// 4. Get single deployment (200 & 404)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/deployments/"+depResp.DeploymentID, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on get deployment, got %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/deployments/missing", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 on missing deployment, got %d", rec.Code)
	}
}
