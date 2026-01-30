package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/binary"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/cache/filehash"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/config"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/database"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/handlers/agent"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/handlers/tls"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/routes"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/rule"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/services"
	retentionsvc "github.com/ZerkerEOD/krakenhashes/backend/internal/services/retention"
	tlsprovider "github.com/ZerkerEOD/krakenhashes/backend/internal/tls"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/version"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/wordlist"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/joho/godotenv"
	"github.com/robfig/cron/v3"
)

func main() {

	// Initialize debug package first with default settings
	debug.Reinitialize()
	debug.Info("Debug logging initialized with default settings")

	// Get current working directory
	cwd, err := os.Getwd()
	if err != nil {
		debug.Error("Failed to get working directory: %v", err)
		os.Exit(1)
	}
	debug.Debug("Current working directory: %s", cwd)

	// Load .env file
	err = godotenv.Load()
	if err != nil {
		debug.Info("Attempting to load .env from current directory: %s", cwd)
		debug.Warning("Failed to load .env file from current directory: %v", err)

		debug.Info("Attempting to load .env from project root")
		err = godotenv.Load("../.env")
		if err != nil {
			debug.Warning("No .env file found, checking environment variables")

			// Check required environment variables
			requiredVars := []string{
				"DB_HOST", "DB_PORT", "DB_USER", "DB_PASSWORD", "DB_NAME",
				"KH_TLS_MODE",
			}

			missingVars := []string{}
			for _, v := range requiredVars {
				if os.Getenv(v) == "" {
					missingVars = append(missingVars, v)
				}
			}

			if len(missingVars) > 0 {
				debug.Error("Missing required environment variables: %v", missingVars)
				debug.Error("Please provide these variables either in a .env file or as environment variables")
				os.Exit(1)
			}

			debug.Info("All required environment variables are present")
		} else {
			debug.Info("Successfully loaded .env file from project root")
		}
	} else {
		debug.Info("Successfully loaded .env file from current directory")
	}

	// Reinitialize debug package with environment variables
	debug.Reinitialize()
	debug.Info("Debug logging initialized with environment settings")

	// Load version information
	debug.Info("Loading version information...")
	// Try different paths for versions.json
	versionPaths := []string{
		"/usr/local/share/krakenhashes/versions.json",              // Non-persistent container location
		"/etc/krakenhashes/versions.json",                          // Config directory (for bare metal installs)
		"../versions.json",                                         // From backend directory
		"versions.json",                                            // From current directory
		"../backend/versions.json",                                 // From project root
		filepath.Join(os.Getenv("KH_CONFIG_DIR"), "versions.json"), // From configured config directory
	}

	var versionPath string
	for _, path := range versionPaths {
		if path == "" {
			continue // Skip empty paths (in case KH_CONFIG_DIR is not set)
		}
		if _, err := os.Stat(path); err == nil {
			versionPath = path
			debug.Info("Found version file at: %s", path)
			break
		}
		debug.Debug("Version file not found at: %s", path)
	}

	if versionPath == "" {
		debug.Error("Version file not found in any of the expected locations. Checked:\n%s",
			"- /usr/local/share/krakenhashes/versions.json\n"+
				"- /etc/krakenhashes/versions.json\n"+
				"- "+filepath.Join(cwd, "../versions.json")+"\n"+
				"- "+filepath.Join(cwd, "versions.json")+"\n"+
				"- "+filepath.Join(cwd, "../backend/versions.json")+"\n"+
				"- "+filepath.Join(os.Getenv("KH_CONFIG_DIR"), "versions.json"))
		os.Exit(1)
	}

	if err := version.LoadVersions(versionPath); err != nil {
		debug.Error("Failed to load version information: %v", err)
		os.Exit(1)
	}
	debug.Info("KrakenHashes Backend v%s starting up", version.Version)
	debug.Info("Component versions - Frontend: %s, Agent: %s, API: %s, Database: %s",
		version.Versions.Frontend,
		version.Versions.Agent,
		version.Versions.API,
		version.Versions.Database)

	debug.Info("Initializing application...")

	// Initialize application configuration
	appConfig := config.NewConfig()
	debug.Info("Application configuration initialized")

	// Initialize TLS provider
	debug.Info("Initializing TLS provider")
	tlsProvider, err := tlsprovider.InitializeProvider(appConfig)
	if err != nil {
		debug.Error("Failed to initialize TLS provider: %v", err)
		os.Exit(1)
	}

	// Start auto-renewal for certbot mode (if enabled)
	if certbotProvider, ok := tlsProvider.(*tlsprovider.CertbotProvider); ok {
		certbotProvider.StartAutoRenewal()
		debug.Info("Auto-renewal started for certbot mode")
	}

	// Get TLS configuration for server
	serverTLSConfig, err := tlsProvider.GetTLSConfig()
	if err != nil {
		debug.Error("Failed to get TLS configuration: %v", err)
		os.Exit(1)
	}

	// Initialize database connection
	debug.Info("Initializing database connection")
	sqlDB, err := database.Connect()
	if err != nil {
		debug.Error("Failed to connect to database: %v", err)
		os.Exit(1)
	}
	defer sqlDB.Close()

	// Create DB wrapper for repositories
	dbWrapper := &db.DB{DB: sqlDB}

	// Initialize repositories and services
	debug.Debug("Initializing repositories and services")
	agentRepo := repository.NewAgentRepository(dbWrapper)
	deviceRepo := repository.NewAgentDeviceRepository(dbWrapper)
	clientRepo := repository.NewClientRepository(dbWrapper)
	clientSettingsRepo := repository.NewClientSettingsRepository(dbWrapper)
	hashlistRepo := repository.NewHashListRepository(dbWrapper)
	hashRepo := repository.NewHashRepository(dbWrapper)
	systemSettingsRepo := repository.NewSystemSettingsRepository(dbWrapper)
	jobExecutionRepo := repository.NewJobExecutionRepository(dbWrapper)
	jobTaskRepo := repository.NewJobTaskRepository(dbWrapper)
	jobIncrementLayerRepo := repository.NewJobIncrementLayerRepository(dbWrapper)
	presetJobRepo := repository.NewPresetJobRepository(sqlDB)
	workflowRepo := repository.NewJobWorkflowRepository(sqlDB)

	// Initialize services with dependencies
	agentService := services.NewAgentService(agentRepo, repository.NewClaimVoucherRepository(dbWrapper), repository.NewFileRepository(dbWrapper, appConfig.DataDir), deviceRepo, jobTaskRepo, jobExecutionRepo)

	analyticsRepo := repository.NewAnalyticsRepository(dbWrapper)
	retentionService := retentionsvc.NewRetentionService(dbWrapper, hashlistRepo, hashRepo, clientRepo, clientSettingsRepo, analyticsRepo)

	// Initialize wordlist and rule managers for monitoring
	wordlistStore := wordlist.NewStore(sqlDB)
	wordlistManager := wordlist.NewManager(
		wordlistStore,
		filepath.Join(appConfig.DataDir, "wordlists"),
		0, // No file size limit
		[]string{"txt", "dict", "lst", "gz", "zip"},                   // Allowed formats
		[]string{"text/plain", "application/gzip", "application/zip"}, // Allowed MIME types
		jobExecutionRepo, // Pass job execution repository for dependency checking
		presetJobRepo,    // Pass preset job repository for cascade deletion
		workflowRepo,     // Pass workflow repository for cascade deletion
	)

	ruleStore := rule.NewStore(sqlDB)
	ruleManager := rule.NewManager(
		ruleStore,
		filepath.Join(appConfig.DataDir, "rules"),
		0,                                       // No file size limit
		[]string{"rule", "rules", "txt", "lst"}, // Allowed formats
		[]string{"text/plain"},                  // Allowed MIME types
		jobExecutionRepo, // Pass job execution repository for dependency checking
		presetJobRepo,    // Pass preset job repository for cascade deletion
		workflowRepo,     // Pass workflow repository for cascade deletion
	)

	// Initialize binary manager
	binaryStore := binary.NewStore(sqlDB)
	binaryDataDir := filepath.Join(appConfig.DataDir, "binaries")
	debug.Info("Configuring binary manager with DataDir: %s", binaryDataDir)
	debug.Info("Current working directory: %s", cwd)
	debug.Info("AppConfig.DataDir: %s", appConfig.DataDir)

	binaryConfig := binary.Config{
		DataDir: binaryDataDir,
	}
	binaryManager, err := binary.NewManager(binaryStore, binaryConfig)
	if err != nil {
		debug.Error("Failed to create binary manager: %v", err)
		os.Exit(1)
	}

	// Run migrations first
	if err := database.RunMigrations(); err != nil {
		debug.Error("Database migrations failed: %v", err)
		os.Exit(1)
	}
	debug.Info("Database migrations completed successfully")

	// Initialize file hash cache for directory monitoring
	// This cache reduces disk I/O by only recalculating MD5 hashes when files change
	debug.Info("Initializing file hash cache...")
	fileHashCache := filehash.New()

	// Start background cache population (non-blocking)
	fileHashCache.PopulateAsync(
		[]string{
			filepath.Join(appConfig.DataDir, "wordlists"),
			filepath.Join(appConfig.DataDir, "rules"),
		},
		[]string{"potfile.txt", "association/"}, // Skip patterns
	)
	debug.Info("File hash cache initialized, background population started")

	// Initialize potfile hash history for handling race conditions during heavy ingestion
	potfileHistory := filehash.NewPotfileHistory(5 * time.Minute)
	debug.Info("Potfile hash history initialized (5-minute window)")

	// Add a small delay to ensure migrations are fully applied
	debug.Info("Waiting for migrations to be fully applied...")
	time.Sleep(10 * time.Second)

	// Ensure the system user exists
	if err := database.EnsureSystemUser(); err != nil {
		debug.Error("Failed to ensure system user exists: %v", err)
		os.Exit(1)
	}
	debug.Info("System user verified")

	// Initialize agent cleanup service and mark all agents as inactive on startup
	debug.Info("Creating agent cleanup service...")
	agentCleanupService := services.NewAgentCleanupService(agentRepo)
	debug.Info("Agent cleanup service created, marking all agents as inactive...")
	if err := agentCleanupService.MarkAllAgentsInactive(context.Background()); err != nil {
		debug.Error("Failed to mark all agents as inactive: %v", err)
		// Don't exit - this is not fatal, but log the error
	} else {
		debug.Info("All agents marked as inactive successfully")
	}

	// Start periodic stale agent cleanup
	go func() {
		ticker := time.NewTicker(1 * time.Minute) // Check every minute
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := agentCleanupService.CleanupStaleAgents(context.Background(), 90*time.Second); err != nil {
					debug.Error("Failed to cleanup stale agents: %v", err)
				}
			}
		}
	}()

	// Initialize job cleanup service and clean up stale tasks
	debug.Info("Creating job cleanup service...")
	jobCleanupService := services.NewJobCleanupService(jobExecutionRepo, jobTaskRepo, systemSettingsRepo, agentRepo)
	debug.Info("Job cleanup service created, starting cleanup of stale tasks from previous runs...")
	cleanupErr := jobCleanupService.CleanupStaleTasksOnStartup(context.Background())
	if cleanupErr != nil {
		debug.Error("Failed to cleanup stale tasks: %v", cleanupErr)
		// Don't exit - this is not fatal
	} else {
		debug.Info("Stale task cleanup completed successfully")
	}

	// Start periodic stale task monitor
	go jobCleanupService.MonitorStaleTasksPeriodically(context.Background(), 5*time.Minute)

	// Use the system user (uuid.Nil) for the monitor service
	systemUserID := uuid.Nil
	debug.Info("Using system user ID for monitor service: %s", systemUserID.String())

	// Initialize job update service
	jobUpdateService := services.NewJobUpdateService(
		presetJobRepo,
		jobExecutionRepo,
		jobTaskRepo,
	)

	// Initialize monitor service
	monitorService := services.NewMonitorService(
		wordlistManager,
		ruleManager,
		appConfig,
		systemUserID,
		jobUpdateService,
		fileHashCache,
	)

	// Initialize and start the Retention Purge Scheduler
	debug.Info("Initializing data retention purge scheduler...")
	cr := cron.New()
	_, err = cr.AddFunc("@daily", func() { // Run once a day at midnight
		debug.Info("Running scheduled data retention purge...")
		if err := retentionService.PurgeOldHashlists(context.Background()); err != nil {
			debug.Error("Scheduled hashlist retention purge failed: %v", err)
		}
		if err := retentionService.PurgeOldAnalyticsReports(context.Background()); err != nil {
			debug.Error("Scheduled analytics report retention purge failed: %v", err)
		}
	})
	if err != nil {
		debug.Error("Failed to add retention purge job to scheduler: %v", err)
		// Decide if this is fatal? For now, log and continue.
	}
	cr.Start()
	debug.Info("Data retention purge scheduler started.")

	// Run initial purge on startup (in background to not block startup)
	go func() {
		debug.Info("Running initial data retention purge on startup...")
		time.Sleep(15 * time.Second) // Small delay after startup
		if err := retentionService.PurgeOldHashlists(context.Background()); err != nil {
			debug.Error("Initial hashlist retention purge failed: %v", err)
		}
		if err := retentionService.PurgeOldAnalyticsReports(context.Background()); err != nil {
			debug.Error("Initial analytics report retention purge failed: %v", err)
		}
	}()

	// Initialize and start token cleanup service
	debug.Info("Creating token cleanup service...")
	tokenCleanupService := services.NewTokenCleanupService(dbWrapper)
	debug.Info("Starting token cleanup service...")
	tokenCleanupService.Start(context.Background())
	defer tokenCleanupService.Stop()
	debug.Info("Token cleanup service started")

	// Initialize client potfile repository (needed by both services)
	clientPotfileRepo := repository.NewClientPotfileRepository(dbWrapper)

	// Initialize pot-file service (unified: handles both global and client potfiles)
	debug.Info("=== POT-FILE SERVICE INITIALIZATION STARTING ===")
	debug.Info("About to initialize pot-file service")
	debug.Info("Initializing pot-file service...")
	potfileService := services.NewPotfileService(
		dbWrapper,
		appConfig.DataDir,
		systemSettingsRepo,
		presetJobRepo,
		wordlistStore,
		hashRepo,
		jobUpdateService,
		potfileHistory,
		clientRepo,        // For client potfile settings lookup
		clientPotfileRepo, // For client potfile metadata
	)

	// Start pot-file service
	if err := potfileService.Start(context.Background()); err != nil {
		debug.Error("Failed to start pot-file service: %v", err)
		// Continue without pot-file service - not fatal
	} else {
		debug.Info("Pot-file service started successfully")
		defer potfileService.Stop()
	}

	// Initialize client-specific potfile service (thin wrapper, delegates to PotfileService)
	debug.Info("Initializing client potfile service...")
	clientPotfileService := services.NewClientPotfileService(
		appConfig.DataDir,
		clientPotfileRepo,
		potfileService, // Delegate to unified PotfileService
	)

	// Start client potfile service
	if err := clientPotfileService.Start(context.Background()); err != nil {
		debug.Error("Failed to start client potfile service: %v", err)
		// Continue without client potfile service - not fatal
	} else {
		debug.Info("Client potfile service started successfully")
		defer clientPotfileService.Stop()
	}

	// Initialize analytics queue service
	debug.Info("Initializing analytics queue service...")
	analyticsService := services.NewAnalyticsService(analyticsRepo)
	analyticsQueueService := services.NewAnalyticsQueueService(analyticsService, analyticsRepo)

	// Start analytics queue service
	if err := analyticsQueueService.Start(); err != nil {
		debug.Error("Failed to start analytics queue service: %v", err)
		// Continue without analytics queue service - not fatal
	} else {
		debug.Info("Analytics queue service started successfully")
		defer analyticsQueueService.Stop()
	}

	// Create routers
	debug.Info("Creating routers")
	httpRouter := mux.NewRouter()  // For HTTP server (CA certificate)
	httpsRouter := mux.NewRouter() // For HTTPS server (API)

	// Apply global CORS middleware to both routers
	httpRouter.Use(routes.GlobalCORSMiddleware)
	httpsRouter.Use(routes.GlobalCORSMiddleware)

	// Setup routes
	debug.Info("Setting up routes")
	routes.SetupRoutes(httpsRouter, sqlDB, tlsProvider, agentService, wordlistManager, ruleManager, binaryManager, potfileService, clientPotfileService, analyticsQueueService)

	// Setup CA certificate route on HTTP router
	debug.Info("Setting up CA certificate route")
	tlsHandler := tls.NewHandler(tlsProvider)
	httpRouter.HandleFunc("/ca.crt", tlsHandler.ServeCACertificate).Methods("GET", "HEAD", "OPTIONS")

	// Setup certificate renewal route
	debug.Info("Setting up certificate renewal route")
	certRenewalHandler := agent.NewCertificateRenewalHandler(tlsProvider, agentRepo)
	httpRouter.HandleFunc("/api/agent/renew-certificates", certRenewalHandler.HandleCertificateRenewal).Methods("POST", "OPTIONS")

	// Also add CA certificate route to HTTPS router for secure access
	httpsRouter.HandleFunc("/ca.crt", tlsHandler.ServeCACertificate).Methods("GET", "HEAD", "OPTIONS")

	// Create HTTPS server
	debug.Info("Creating HTTPS server")
	httpsServer := &http.Server{
		Addr:      appConfig.GetHTTPSAddress(),
		Handler:   httpsRouter,
		TLSConfig: serverTLSConfig,
	}

	// Create HTTP server for CA certificate
	httpServer := &http.Server{
		Addr:    appConfig.GetHTTPAddress(),
		Handler: httpRouter,
	}

	// Start HTTP server in a goroutine for CA certificate
	go func() {
		debug.Info("Starting HTTP server for CA certificate on %s", httpServer.Addr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			debug.Error("HTTP server error: %v", err)
		}
	}()

	// Channel to wait for server errors
	serverErr := make(chan error, 1)

	// Start HTTPS server in a goroutine
	go func() {
		debug.Info("Starting HTTPS server on %s", httpsServer.Addr)
		if err := httpsServer.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
			debug.Error("HTTPS server error: %v", err)
			serverErr <- err
		}
	}()

	// Wait a moment for servers to start
	time.Sleep(500 * time.Millisecond)

	// Start monitor service after servers and database are ready
	debug.Info("Starting directory monitor service")
	monitorService.Start()
	defer monitorService.Stop()

	// Start the job scheduler if it was initialized
	if routes.JobIntegrationManager != nil {
		debug.Info("Starting job scheduler")
		jobSchedulerCtx, jobSchedulerCancel := context.WithCancel(context.Background())
		defer jobSchedulerCancel()
		routes.JobIntegrationManager.StartScheduler(jobSchedulerCtx)
		debug.Info("Job scheduler started successfully")
	} else {
		debug.Warning("Job integration manager not initialized, job scheduler will not start")
	}

	// Initialize and start job progress calculation service
	debug.Info("Starting job progress calculation service...")
	jobProgressCalcService := services.NewJobProgressCalculationService(dbWrapper, jobExecutionRepo, jobTaskRepo, jobIncrementLayerRepo)
	jobProgressCalcService.Start()
	defer jobProgressCalcService.Stop()
	debug.Info("Job progress calculation service started successfully")

	// Wait for interrupt signal or server error
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	debug.Info("Server is ready to handle requests")

	// Block until we receive a signal or server error
	select {
	case err := <-serverErr:
		debug.Error("Server error: %v", err)
		os.Exit(1)
	case sig := <-sigChan:
		debug.Info("Received signal: %v", sig)
		debug.Info("Shutting down server...")

		// Create a deadline for graceful shutdown
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		// Shutdown both servers
		if err := httpServer.Shutdown(ctx); err != nil {
			debug.Error("Error during HTTP server shutdown: %v", err)
		}
		if err := httpsServer.Shutdown(ctx); err != nil {
			debug.Error("Error during HTTPS server shutdown: %v", err)
		}
		debug.Info("Server shutdown complete")
	}
}
