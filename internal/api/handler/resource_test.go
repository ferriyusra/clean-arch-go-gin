package handler_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"go.uber.org/mock/gomock"

	"github.com/ferriyusra/clean-arch-go-gin/internal/api/handler"
	"github.com/ferriyusra/clean-arch-go-gin/internal/apperr"
	"github.com/ferriyusra/clean-arch-go-gin/internal/model/response"
	"github.com/ferriyusra/clean-arch-go-gin/internal/service/mock"
	"github.com/ferriyusra/clean-arch-go-gin/internal/testutil"
)

func TestCounterHandler(t *testing.T) {
	t.Run("returns the current value", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		svc := mock.NewMockCounterService(ctrl)
		svc.EXPECT().GetCounter(gomock.Any()).Return(&response.GetCounter{Value: 7}, nil)

		r := testutil.NewEngine(t)
		r.GET("/api/counter", handler.NewCounterHandler(svc).GetCounter)

		rec := testutil.Do(r, testutil.JSONRequest(t, http.MethodGet, "/api/counter", nil))

		testutil.Equal(t, rec.Code, http.StatusOK, "status")
		testutil.Equal(t, testutil.DataAs[response.GetCounter](t, rec).Value, 7, "value")
	})

	t.Run("increments and returns the new value", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		svc := mock.NewMockCounterService(ctrl)
		svc.EXPECT().IncrementCounter(gomock.Any()).Return(&response.GetCounter{Value: 8}, nil)

		r := testutil.NewEngine(t)
		r.POST("/api/counter", handler.NewCounterHandler(svc).IncrementCounter)

		rec := testutil.Do(r, testutil.JSONRequest(t, http.MethodPost, "/api/counter", nil))

		testutil.Equal(t, rec.Code, http.StatusOK, "status")
		testutil.Equal(t, testutil.DataAs[response.GetCounter](t, rec).Value, 8, "value")
	})

	t.Run("hides the internal cause of a failure", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		svc := mock.NewMockCounterService(ctrl)
		svc.EXPECT().GetCounter(gomock.Any()).
			Return(nil, apperr.Internal(errors.New("SELECT counters: connection refused")))

		r := testutil.NewEngine(t)
		r.GET("/api/counter", handler.NewCounterHandler(svc).GetCounter)

		rec := testutil.Do(r, testutil.JSONRequest(t, http.MethodGet, "/api/counter", nil))

		testutil.Equal(t, rec.Code, http.StatusInternalServerError, "status")
		testutil.Equal(t, testutil.Envelope(t, rec).Message, "Internal server error", "message")
		testutil.True(t, !strings.Contains(rec.Body.String(), "connection refused"), "SQL error absent from body")
	})
}

func TestMessageHandler(t *testing.T) {
	ctrl := gomock.NewController(t)
	svc := mock.NewMockMessageService(ctrl)
	svc.EXPECT().GetMessage(gomock.Any()).Return(&response.GetMessage{Content: "hello"}, nil)

	r := testutil.NewEngine(t)
	r.GET("/api/message", handler.NewMessageHandler(svc).GetMessage)

	rec := testutil.Do(r, testutil.JSONRequest(t, http.MethodGet, "/api/message", nil))

	testutil.Equal(t, rec.Code, http.StatusOK, "status")
	testutil.Equal(t, testutil.DataAs[response.GetMessage](t, rec).Content, "hello", "content")
}

func TestHealthHandlerLiveness(t *testing.T) {
	ctrl := gomock.NewController(t)
	svc := mock.NewMockHealthService(ctrl)
	svc.EXPECT().Check(gomock.Any()).Return(&response.HealthStatus{Status: "ok"}, nil)

	r := testutil.NewEngine(t)
	r.GET("/api/health/live", handler.NewHealthHandler(svc, nil).Check)

	rec := testutil.Do(r, testutil.JSONRequest(t, http.MethodGet, "/api/health/live", nil))

	testutil.Equal(t, rec.Code, http.StatusOK, "status")
}

func TestHealthHandlerReadiness(t *testing.T) {
	tests := []struct {
		name       string
		status     *response.HealthStatus
		wantStatus int
	}{
		{
			name:       "reports ready when every dependency answers",
			status:     &response.HealthStatus{Status: "ok", Message: "All dependencies healthy"},
			wantStatus: http.StatusOK,
		},
		{
			// 503 is what tells a load balancer to stop sending traffic here;
			// the old single endpoint always answered 200.
			name:       "reports 503 when a dependency is down",
			status:     &response.HealthStatus{Status: "degraded", Message: "1 dependency checks failed"},
			wantStatus: http.StatusServiceUnavailable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			svc := mock.NewMockHealthService(ctrl)
			svc.EXPECT().CheckWithDependencies(gomock.Any(), gomock.Any()).Return(tt.status, nil)

			checks := handler.DependencyChecks{
				"database": func(context.Context) error { return nil },
			}

			r := testutil.NewEngine(t)
			r.GET("/api/health", handler.NewHealthHandler(svc, checks).Ready)

			rec := testutil.Do(r, testutil.JSONRequest(t, http.MethodGet, "/api/health", nil))

			testutil.Equal(t, rec.Code, tt.wantStatus, "status")
		})
	}
}
