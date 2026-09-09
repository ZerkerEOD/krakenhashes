package main

import (
	"context"
	// Aliased: internal/handlers/tls below already takes the name `tls`.
	cryptotls "crypto/tls"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	/*
	 * Embed the IANA zone database in the binary (~450 KB) rather than trusting
	 * the host to have /usr/share/zoneinfo.
	 *
	 * Provisioning windows are stored as an IANA name and evaluated with
	 * time.LoadLocation, so on a host without the zone files EVERY name fails
	 * to load. The failure is fail-closed by design — an unreadable window
	 * refuses rather than rents — which means a missing OS package silently
	 * stops all cloud provisioning for every client that configured a window,
	 * and blocks the admin from fixing it, since UpsertRules validates zone
	 * names the same way. The Dockerfiles do install tzdata; this makes the
	 * binary correct anywhere it runs, including scratch images and a
	 * developer's `go run`.
	 *
	 * Embedded data is only consulted when the host has no zone files, so this
	 * does not override a deliberately patched system database.
	 */
	_ "time/tzdata"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/binary"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/cache/filehash"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/config"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/database"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/db"
	admincloud "github.com/ZerkerEOD/krakenhashes/backend/internal/handlers/admin/cloud"
	adminsettings "github.com/ZerkerEOD/krakenhashes/backend/internal/handlers/admin/settings"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/handlers/agent"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/handlers/tls"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/middleware"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/repository"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/routes"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/rule"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/services"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/services/certs"
	cloudsvc "github.com/ZerkerEOD/krakenhashes/backend/internal/services/cloud"
	retentionsvc "github.com/ZerkerEOD/krakenhashes/backend/internal/services/retention"
	"github.com/ZerkerEOD/krakenhashes/backend/internal/services/sandiscovery"
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
	//
	// godotenv aborts on the FIRST unparseable line and returns without applying
	// anything, so a single malformed line silently discards every variable in
	// the file. That failure mode is indistinguishable from "no .env present"
	// unless we check separately, and it has already cost real debugging time:
	// a stray shell conditional written into .env by the container entrypoint
	// dropped KH_ADDITIONAL_IP_ADDRESSES for every dev deployment. A present but
	// broken .env is an error, not a missing-file fallback.
	err = godotenv.Load()
	if err != nil {
		if _, statErr := os.Stat(".env"); statErr == nil {
			debug.Error("A .env file exists in %s but could not be parsed: %v", cwd, err)
			debug.Error("EVERY variable in that file has been ignored, not just the offending line.")
			debug.Error("Fix the line named above; environment variables are being used instead.")
		}

		debug.Info("Attempting to load .env from current directory: %s", cwd)
		debug.Warning("Failed to load .env file from current directory: %v", err)

		debug.Info("Attempting to load .env from project root")
		err = godotenv.Load("../.env")
		if err != nil {
			if _, statErr := os.Stat("../.env"); statErr == nil {
				debug.Error("A .env file exists at ../.env but could not be parsed: %v", err)
				debug.Error("EVERY variable in that file has been ignored, not just the offending line.")
			}

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
	debug.Info("KrakenHashes Backend v%s starting up", version.BackendVersion())
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

	// Refuse to run a second backend against the same database.
	//
	// The scheduler's single-flight guard is process-local, so two backends
	// both run the 3-second cycle and double-dispatch the same keyspace
	// intervals. Acquired before migrations so two processes can't race those
	// either. Waits briefly for an outgoing process during a rolling restart.
	instanceLock, err := database.AcquireInstanceLock(context.Background(), sqlDB)
	if err != nil {
		debug.Error("%v", err)
		os.Exit(1)
	}
	defer func() {
		if relErr := instanceLock.Release(context.Background()); relErr != nil {
			debug.Warning("Failed to release single-instance lock: %v", relErr)
		}
	}()

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
	// Wire the scheduling-diagnostics repo so the agent detail page can show
	// why an agent is idle (binary mismatch, blocklisted, etc.). The scheduler
	// writes these reasons via its buffered DiagnosticsService; here we only read.
	agentService.SetDiagnosticsRepo(repository.NewDiagnosticsRepository(dbWrapper))

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
		jobExecutionRepo,                        // Pass job execution repository for dependency checking
		presetJobRepo,                           // Pass preset job repository for cascade deletion
		workflowRepo,                            // Pass workflow repository for cascade deletion
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

	// Reconcile the server certificate with the configured subject alternative
	// names.
	//
	// Placed here for two reasons that are not stylistic:
	//
	//   - It must run after migrations, because the SAN settings rows do not
	//     exist on a fresh database until they are seeded. systemSettingsRepo is
	//     constructed above, before migrations run, so the check cannot go there.
	//   - It must run well before the HTTPS listener starts, so that a
	//     certificate missing an agent's address is replaced before that agent
	//     can fail a handshake against it.
	//
	// Non-fatal by design: a deployment that cannot reissue still boots on its
	// existing certificate so an administrator can log in and fix it.
	certService := certs.New(tlsProvider, systemSettingsRepo, appConfig)
	certService.EnsureAtStartup(context.Background())

	// Certificate-name discovery. Aggregates in memory and flushes periodically
	// so the request path never performs a database write.
	sanCandidateRepo := repository.NewTLSSANCandidateRepository(dbWrapper)
	sanCache := sandiscovery.NewCache(sanCandidateRepo)
	certService.AttachDiscovery(sanCache, sanCandidateRepo)
	certService.RefreshDiscoveryIgnoreSet()
	go sanCache.Run(context.Background())

	// Observe the server name from TLS ClientHellos. Set here rather than inside
	// GetTLSConfig because it needs the discovery cache, which needs a database.
	// Returning nil keeps the base configuration; this hook only watches.
	serverTLSConfig.GetConfigForClient = func(hi *cryptotls.ClientHelloInfo) (*cryptotls.Config, error) {
		certService.ObserveSNI(hi.ServerName)
		return nil, nil
	}

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

	// NOTE: the periodic stale-task monitor is NOT started here. Its
	// stale-processing backstop needs the WebSocket integration, which
	// routes.SetupRoutes has not built yet at this point in startup; see the
	// SetStuckProcessingHandler call further down.

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

	// Record the Host header of inbound requests as candidate certificate names.
	//
	// Registered on BOTH routers deliberately. The HTTP router on the bootstrap
	// port serves /ca.crt, which an agent fetches before it has any working TLS
	// at all -- for an agent whose handshake is failing, that request is the only
	// passive observation this server ever gets.
	httpRouter.Use(middleware.SANObserver(sanCache))
	httpsRouter.Use(middleware.SANObserver(sanCache))

	// Setup routes
	debug.Info("Setting up routes")
	routes.SetupRoutes(httpsRouter, sqlDB, tlsProvider, agentService, wordlistManager, ruleManager, binaryManager, potfileService, clientPotfileService, analyticsQueueService)

	// Wire the scheduler compat-cache invalidator into the agent service now
	// that SetupRoutes has constructed the job integration manager. This makes
	// an admin binary_version change re-evaluate the agent's scheduling
	// compatibility immediately instead of after the next periodic re-warm.
	if routes.JobIntegrationManager != nil {
		agentService.SetCompatInvalidator(routes.JobIntegrationManager.InvalidateAgentCompat)

		// Wire the stale-processing backstop to the WebSocket integration. This
		// is the first point in startup where that instance exists, which is
		// why the periodic monitor below is started here rather than next to
		// the cleanup service's construction: started earlier, its two gates
		// (TryFinalizeTask / AbandonProcessingTask) would be nil for the whole
		// first tick or more, and a task stuck in 'processing' would keep
		// sitting there.
		if wsIntegration := routes.JobIntegrationManager.GetWebSocketIntegration(); wsIntegration != nil {
			jobCleanupService.SetStuckProcessingHandler(wsIntegration)
		} else {
			debug.Warning("WebSocket integration unavailable - stale-processing backstop will run inert (stuck 'processing' tasks will be logged, not recovered)")
		}
	} else {
		debug.Warning("Job integration manager not initialized - stale-processing backstop will run inert (stuck 'processing' tasks will be logged, not recovered)")
	}

	// Start periodic stale task monitor. context.Background() preserved from
	// the original call site: this sweep is meant to outlive every request
	// context and stop only when the process does.
	go jobCleanupService.MonitorStaleTasksPeriodically(context.Background(), 5*time.Minute)

	// Setup CA certificate route on HTTP router
	debug.Info("Setting up CA certificate route")
	tlsHandler := tls.NewHandler(tlsProvider)
	httpRouter.HandleFunc("/ca.crt", tlsHandler.ServeCACertificate).Methods("GET", "HEAD", "OPTIONS")

	// Setup certificate renewal route
	debug.Info("Setting up certificate renewal route")
	certRenewalHandler := agent.NewCertificateRenewalHandler(tlsProvider, agentRepo)
	httpRouter.HandleFunc("/api/agent/renew-certificates", certRenewalHandler.HandleCertificateRenewal).Methods("POST", "OPTIONS")

	// Agent TLS-failure reporting.
	//
	// HTTP router only, and that is the whole point: an agent that can reach the
	// HTTPS API does not have this problem. This is the one channel still open to
	// an agent whose TLS handshake fails, and it is how the address that agent is
	// actually dialling reaches the administrator.
	debug.Info("Setting up agent TLS failure reporting route")
	tlsFailureHandler := agent.NewTLSFailureHandler(agentRepo, sanCache)
	httpRouter.HandleFunc("/api/agent/tls-failure", tlsFailureHandler.HandleTLSFailure).Methods("POST", "OPTIONS")

	// Also add CA certificate route to HTTPS router for secure access
	httpsRouter.HandleFunc("/ca.crt", tlsHandler.ServeCACertificate).Methods("GET", "HEAD", "OPTIONS")

	// ---------------------------------------------------------------------
	// Cloud GPU provisioning
	// ---------------------------------------------------------------------
	//
	// Placed here, before the servers start and before StartScheduler below,
	// for two reasons that are not stylistic:
	//
	//   - Registering routes on a mux.Router that is already serving is a data
	//     race, so the admin API must be attached first.
	//   - SetCloudAgentLocks must be wired before the scheduler's first cycle.
	//     Without it the compat wrapper has no lock snapshot, and a rented
	//     agent could be handed another client's job.
	//
	// The reaper starts even when no provider is configured: its first pass is
	// the startup reconciliation that reclaims anything the previous process
	// left running. Skipping it because "cloud is off right now" would strand
	// instances rented before the feature was disabled.
	debug.Info("Starting cloud provisioning services...")
	claimVoucherService := services.NewClaimVoucherService(repository.NewClaimVoucherRepository(dbWrapper))
	cloudProviderRepo := repository.NewCloudProviderRepository(dbWrapper)
	cloudInstanceRepo := repository.NewCloudInstanceRepository(dbWrapper)
	cloudBudgetRepo := repository.NewCloudBudgetRepository(dbWrapper)
	cloudBudget := cloudsvc.NewBudgetEngine(cloudBudgetRepo)
	cloudFileSets := cloudsvc.NewFileSetResolver(dbWrapper)
	cloudService := cloudsvc.NewService(
		dbWrapper, cloudProviderRepo, cloudInstanceRepo, cloudBudget,
		cloudFileSets, claimVoucherService,
	)
	// Bootstrap value only. The database setting cloud_agent_image is
	// authoritative once configured; this is the fallback and the source the
	// one-time import below copies from.
	cloudService.AgentImage = getEnvOrDefault(cloudsvc.EnvAgentImage, cloudsvc.DefaultAgentImage)
	cloudService.SystemUserID = models.SystemUserID.String()
	// The deployment-wide spend ceiling lives in system_settings. Without this
	// the service cannot read it and refuses to provision at all, which is the
	// correct behaviour for an unreadable kill switch but not what we want here.
	cloudService.SystemSettings = systemSettingsRepo

	// Move KH_CLOUD_AGENT_IMAGE into the database once, so the admin UI shows
	// the image that is actually in effect rather than an empty field beside a
	// value only the host's environment knows about.
	cloudsvc.ImportEnvAgentImageIfUnset(context.Background(), systemSettingsRepo)

	// Operator-tunable timings. These keys were seeded by the provisioning
	// migration and read by nothing, so an admin who changed them was changing
	// a display value. Loaded once at startup: they govern loop cadence, and
	// re-reading them per tick would put a query on every sweep.
	cloudSettings := cloudsvc.LoadSettings(context.Background(), systemSettingsRepo)
	debug.Info("Cloud settings: reaper interval=%s, orphan grace=%s, idle drain=%s, commissioning grace=%s, instance cap=%d",
		cloudSettings.ReaperInterval, cloudSettings.OrphanGrace, cloudSettings.IdleDrain,
		cloudSettings.CommissioningGrace, cloudSettings.GlobalInstanceCap)

	// The reaper's escalation path exists for one situation: automation has
	// lost control of an instance that is still billing. Passing nil here made
	// that alert dead code. GetGlobalDispatcher is set by SetupNotificationRoutes,
	// which has already run; DispatchNotifier degrades to logging if it has not.
	var cloudNotifier cloudsvc.Notifier
	if dispatcher := services.GetGlobalDispatcher(); dispatcher != nil {
		cloudNotifier = cloudsvc.NewDispatchNotifier(dispatcher)
	} else {
		debug.Warning("Notification dispatcher unavailable - cloud teardown failures will only be logged")
		cloudNotifier = cloudsvc.NewDispatchNotifier(nil)
	}

	cloudReaper := cloudsvc.NewReaper(cloudInstanceRepo, cloudBudget, cloudService.ProviderFor, cloudNotifier)
	cloudReaper.OrphanGrace = cloudSettings.OrphanGrace
	cloudReaper.IdleDrain = cloudSettings.IdleDrain
	cloudReaper.CommissioningGrace = cloudSettings.CommissioningGrace
	cloudCtx, cloudCancel := context.WithCancel(context.Background())
	defer cloudCancel()
	// The environment variable still wins when set, so an operator debugging a
	// stuck teardown can tighten the loop without a database write.
	reaperInterval := cloudSettings.ReaperInterval
	if env := getEnvIntOrDefault("KH_CLOUD_REAPER_INTERVAL", 0); env > 0 {
		reaperInterval = time.Duration(env) * time.Second
	}
	go cloudReaper.Run(cloudCtx, reaperInterval)

	// Pin rented agents to the job that paid for them. Nil-safe: leaving this
	// unset would silently allow a cloud agent to take another client's work.
	if routes.JobIntegrationManager != nil {
		routes.JobIntegrationManager.SetCloudAgentLocks(cloudService)
	}

	// Give rented agents their job's files on connect. Cloud agents are excluded
	// from the full-corpus sync (which would ship every client's potfile to a
	// machine the operator does not control); this is the replacement, not an
	// optimisation.
	if routes.WSHandler != nil {
		routes.WSHandler.SetCloudFileSetResolver(cloudFileSets)
	} else {
		debug.Warning("WebSocket handler unavailable - cloud agents will download files lazily per task")
	}

	// Autoscaler: rents capacity for starving, cloud-eligible jobs.
	//
	// Two caps guard it, and the second is easy to get wrong. GlobalInstanceCap
	// on its own does nothing: the check at Autoscaler.ScaleOnce is skipped
	// unless LiveInstanceCount is also set, so a cap without a counter is
	// silently unlimited. They are set together here for that reason.
	//
	// Runs only when the scheduler exists to feed it. Without the starvation
	// publisher the snapshot never refreshes, every pass reads stale data, and
	// the autoscaler would sit in a permanent no-op — starting it then would
	// only produce log noise suggesting it was doing something.
	if routes.JobIntegrationManager != nil {
		starvation := cloudsvc.NewStarvationSnapshot()
		routes.JobIntegrationManager.SetCloudStarvationPublisher(starvation)

		autoscaler := cloudsvc.NewAutoscaler(starvation, cloudService)
		autoscaler.GlobalInstanceCap = cloudSettings.GlobalInstanceCap
		if env := getEnvIntOrDefault("KH_CLOUD_MAX_INSTANCES", 0); env > 0 {
			autoscaler.GlobalInstanceCap = env
		}
		autoscaler.LiveInstanceCount = cloudInstanceRepo.CountLive

		// Surface provisioning refusals on the job itself. Without this the
		// only record is a server log line, which is no use at all to a
		// cloud-only operator whose jobs are sitting at pending — and the
		// per-agent diagnostics path cannot help them, because it iterates
		// agents and they have none.
		if diag := routes.JobIntegrationManager.DiagnosticsService(); diag != nil {
			autoscaler.Diagnostics = diag
			// Same store, read side: the job detail page renders what the
			// autoscaler records here.
			if routes.UserJobsHandlerInstance != nil {
				routes.UserJobsHandlerInstance.SetDiagnostics(diag)
			}
		}

		// Negative disables the breaker; 0 means "leave the built-in default".
		// A plain >0 test would make KH_CLOUD_DOA_LIMIT=0 a silent no-op rather
		// than the off switch it reads as.
		if env := getEnvIntOrDefault("KH_CLOUD_DOA_LIMIT", 0); env != 0 {
			if env < 0 {
				env = 0
			}
			autoscaler.DeadOnArrivalLimit = env
		}
		if env := getEnvIntOrDefault("KH_CLOUD_GLOBAL_DOA_LIMIT", 0); env != 0 {
			if env < 0 {
				env = 0
			}
			autoscaler.GlobalDeadOnArrivalLimit = env
		}

		interval := time.Duration(getEnvIntOrDefault("KH_CLOUD_AUTOSCALE_INTERVAL", 60)) * time.Second
		go autoscaler.Run(cloudCtx, interval)
		debug.Info("Cloud autoscaler started (interval=%s, global instance cap=%d, dead-on-arrival limit=%d/job, %d deployment-wide)",
			interval, autoscaler.GlobalInstanceCap, autoscaler.DeadOnArrivalLimit, autoscaler.GlobalDeadOnArrivalLimit)
	} else {
		debug.Warning("Scheduler unavailable - cloud autoscaler not started")
	}

	// Admin API. AdminRouter already carries middleware.AdminOnly.
	if routes.AdminRouter != nil {
		admincloud.NewHandler(
			cloudProviderRepo, cloudInstanceRepo, cloudBudgetRepo, cloudBudget,
			cloudsvc.NewEstimator(dbWrapper),
			repository.NewCloudProvisioningRulesRepository(dbWrapper),
			cloudService.ProviderFor,
			cloudService.InvalidateProvider, cloudService.ProvisionForJob,
		).RegisterRoutes(routes.AdminRouter)
		debug.Info("Configured cloud admin routes: /api/admin/cloud/*")
	} else {
		debug.Warning("Admin router unavailable - cloud provisioning admin API not registered")
	}
	debug.Info("Cloud provisioning services started")

	// Server-certificate admin API.
	//
	// Attached here rather than in SetupAdminRoutes because the handler needs the
	// certificate service, which needs the TLS provider and the discovery cache —
	// neither of which SetupAdminRoutes receives.
	if routes.AdminRouter != nil {
		adminsettings.NewCertificateHandler(certService).RegisterRoutes(routes.AdminRouter)
		debug.Info("Configured TLS certificate admin routes: /api/admin/tls/*")
	} else {
		debug.Warning("Admin router unavailable - TLS certificate admin API not registered")
	}

	// Let a CA rotation push the new trust material to agents that are already
	// connected, instead of leaving them with a stale CA until they reconnect.
	// Wired here because the WebSocket handler is built inside SetupRoutes,
	// after the certificate service is constructed.
	if routes.WSHandler != nil {
		certService.AttachAgentNotifier(routes.WSHandler.BroadcastCertRefresh)
	} else {
		debug.Warning("WebSocket handler unavailable - agents will refresh certificates on reconnect only")
	}

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

	// Held at function scope so the ordered shutdown can stop dispatch before
	// the HTTP servers drain, rather than relying on a defer that fires after
	// the shutdown deadline has already been spent.
	var jobSchedulerCancelFn context.CancelFunc

	// Start the job scheduler if it was initialized
	if routes.JobIntegrationManager != nil {
		// One-shot converter: migrate any pre-existing v1 jobs into v2
		// units before the scheduler runs. Jobs whose wordlist or rule
		// refs no longer resolve are deleted. Must run before
		// StartScheduler so the cycle sees a fully-v2 world.
		convCtx, convCancel := context.WithTimeout(context.Background(), 5*time.Minute)
		if err := routes.JobIntegrationManager.ConvertLegacyJobsToV2(convCtx); err != nil {
			debug.Error("Legacy job converter failed: %v", err)
			convCancel()
			os.Exit(1)
		}
		convCancel()

		debug.Info("Starting job scheduler")
		jobSchedulerCtx, jobSchedulerCancel := context.WithCancel(context.Background())
		defer jobSchedulerCancel()
		// Hoisted so the ordered shutdown path can stop dispatch before the
		// HTTP servers drain.
		jobSchedulerCancelFn = jobSchedulerCancel
		routes.JobIntegrationManager.StartScheduler(jobSchedulerCtx)
		debug.Info("Job scheduler started successfully")

		// One-time safety sweep: repair pending jobs that never started and have
		// an inaccurate keyspace (e.g. stranded by the older scheduler-v2
		// bootstrap deadlock) by recomputing it via hashcat
		// --keyspace/--total-candidates. Runs in the background so per-job
		// hashcat execs don't delay boot; the scheduler picks up repaired jobs
		// on its next cycle. Best-effort — failures are logged, not fatal.
		go func() {
			repairCtx, repairCancel := context.WithTimeout(context.Background(), 30*time.Minute)
			defer repairCancel()
			if n, err := routes.JobIntegrationManager.RepairPendingJobKeyspaces(repairCtx); err != nil {
				debug.Warning("Pending-job keyspace repair sweep failed: %v", err)
			} else if n > 0 {
				debug.Info("Pending-job keyspace repair sweep: repaired %d job(s)", n)
			}
		}()
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

		// Ordered shutdown.
		//
		// Cloud teardown runs FIRST, with its own budget. Deferred cleanup
		// would not do: defers fire after this select returns, by which point
		// the 15s HTTP shutdown context below is already spent — leaving
		// rented GPUs billing while we wait on connection draining.
		//
		// Best-effort by nature: SIGKILL and OOM bypass this entirely, which
		// is exactly why the in-guest absolute deadline is the real guarantee
		// and this is only an optimization to stop billing sooner.
		drainCtx, drainCancel := context.WithTimeout(context.Background(), 60*time.Second)
		cloudReaper.DrainAll(drainCtx)
		drainCancel()
		cloudCancel()

		// Stop the scheduler before the servers so no new work is dispatched
		// to agents that are about to lose their connection.
		if jobSchedulerCancelFn != nil {
			jobSchedulerCancelFn()
		}

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

// getEnvOrDefault returns an environment variable or a fallback.
func getEnvOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// getEnvIntOrDefault returns an integer environment variable or a fallback.
func getEnvIntOrDefault(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		debug.Warning("Invalid %s=%q; using default %d", key, v, fallback)
		return fallback
	}
	return n
}
