package routes

import (
	"net/http"

	adminhandlers "github.com/ZerkerEOD/krakenhashes/backend/internal/handlers/admin"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/services"
	"github.com/gorilla/mux"
)

// SetupJobAnalyticsRoutes configures job performance analytics routes on the admin router.
// The admin router already has middleware.AdminOnly applied.
func SetupJobAnalyticsRoutes(adminRouter *mux.Router, database *db.DB) {
	analyticsRepo := repository.NewJobAnalyticsRepository(database)
	benchmarkRepo := repository.NewBenchmarkRepository(database)
	analyticsService := services.NewJobAnalyticsService(analyticsRepo, benchmarkRepo)
	handler := adminhandlers.NewJobAnalyticsHandler(analyticsService)

	adminRouter.HandleFunc("/job-analytics/filters", handler.GetFilters).Methods(http.MethodGet, http.MethodOptions)
	adminRouter.HandleFunc("/job-analytics/summary", handler.GetSummary).Methods(http.MethodGet, http.MethodOptions)
	adminRouter.HandleFunc("/job-analytics/jobs", handler.GetJobs).Methods(http.MethodGet, http.MethodOptions)
	adminRouter.HandleFunc("/job-analytics/timeline", handler.GetTimeline).Methods(http.MethodGet, http.MethodOptions)
	adminRouter.HandleFunc("/job-analytics/jobs/{id:[0-9a-fA-F-]+}/timeline", handler.GetJobTimeline).Methods(http.MethodGet, http.MethodOptions)
	adminRouter.HandleFunc("/job-analytics/success-rates", handler.GetSuccessRates).Methods(http.MethodGet, http.MethodOptions)
	adminRouter.HandleFunc("/job-analytics/benchmarks", handler.GetBenchmarkHistory).Methods(http.MethodGet, http.MethodOptions)
	adminRouter.HandleFunc("/job-analytics/benchmarks/trends", handler.GetBenchmarkTrends).Methods(http.MethodGet, http.MethodOptions)
}
