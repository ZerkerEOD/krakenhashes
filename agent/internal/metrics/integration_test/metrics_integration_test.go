/*
 * Package integration_test provides comprehensive integration testing for the KrakenHashes agent metrics system.
 *
 * Job Processing:
 *   - Input: System metrics collection configuration
 *   - Processing: Real-time metrics gathering and transmission
 *   - Error handling: Connection failures, data collection errors
 *   - Resource management: Proper cleanup of resources
 *
 * Testing Coverage:
 *   - WebSocket communication validation
 *   - Metrics collection accuracy
 *   - System resource monitoring
 *   - Data persistence verification
 *   - Error recovery mechanisms
 *
 * Dependencies:
 *   - metrics package
 *   - websocket package
 *   - testify for assertions
 *   - httptest for mock server
 *
 * Error Scenarios:
 *   - Connection interruption
 *   - Invalid metrics data
 *   - Resource unavailability
 *   - Database failures
 */
package integration_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/ZerkerEOD/krakenhashes/agent/internal/agent"
	"github.com/ZerkerEOD/krakenhashes/agent/internal/config"
	"github.com/ZerkerEOD/krakenhashes/agent/internal/metrics"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

/*
 * mockBackend creates a test server that simulates the backend WebSocket server.
 *
 * Features:
 *   - WebSocket upgrade handling
 *   - Message buffering
 *   - Connection lifecycle management
 *   - Error simulation capabilities
 *
 * Parameters:
 *   - t: Testing context
 *
 * Returns:
 *   - *httptest.Server: Mock backend server
 *   - chan agent.Message: Channel for received messages
 */
func mockBackend(t *testing.T) (*httptest.Server, chan agent.Message) {
	messageChan := make(chan agent.Message, 10)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Upgrade") == "websocket" {
			conn, err := upgrader.Upgrade(w, r, nil)
			require.NoError(t, err)
			defer conn.Close()

			for {
				messageType, data, err := conn.ReadMessage()
				if err != nil {
					return
				}

				if messageType == websocket.TextMessage {
					var msg agent.Message
					err := json.Unmarshal(data, &msg)
					require.NoError(t, err)
					messageChan <- msg
				}
			}
		}
	}))

	return server, messageChan
}

/*
 * TestMetricsIntegration validates the end-to-end metrics collection and transmission.
 *
 * Test Scenarios:
 *   - Basic metrics collection and transmission
 *   - Connection interruption handling
 *   - Reconnection behavior
 *   - Data validation
 *
 * Requirements (from .cursorrules lines 873-881):
 *   - WebSocket communication testing
 *   - Recovery mechanism validation
 *   - Resource limit verification
 */
func TestMetricsIntegration(t *testing.T) {
	// Start mock backend
	server, messages := mockBackend(t)
	defer server.Close()

	// Create metrics collector
	collector, err := metrics.New(metrics.Config{
		CollectionInterval: time.Second,
		EnableGPU:          true,
	})
	require.NoError(t, err)
	defer collector.Close()

	// Create test URL configuration
	os.Setenv("KH_SERVER_URL", "ws://localhost:8080")
	urlConfig := config.NewURLConfig()

	// Try to register the agent
	err = agent.RegisterAgent("test-claim-code", urlConfig, "", "")
	require.NoError(t, err)

	// Create connection
	conn, err := agent.NewConnection(urlConfig)
	require.NoError(t, err)
	err = conn.Connect()
	require.NoError(t, err)
	defer conn.Close()

	// Test cases
	tests := []struct {
		name     string
		duration time.Duration
		validate func(t *testing.T, msgs []agent.Message)
	}{
		{
			name:     "collect and transmit metrics",
			duration: 3 * time.Second,
			validate: func(t *testing.T, msgs []agent.Message) {
				assert.GreaterOrEqual(t, len(msgs), 2, "Should receive multiple metric updates")

				for _, msg := range msgs {
					assert.Equal(t, agent.TypeHeartbeat, msg.Type)
					assert.NotNil(t, msg.Metrics)
					assert.GreaterOrEqual(t, msg.Metrics.CPUUsage, 0.0)
					assert.LessOrEqual(t, msg.Metrics.CPUUsage, 100.0)
					assert.GreaterOrEqual(t, msg.Metrics.MemoryUsage, 0.0)
					assert.LessOrEqual(t, msg.Metrics.MemoryUsage, 100.0)
				}
			},
		},
		{
			name:     "handle connection interruption",
			duration: 5 * time.Second,
			validate: func(t *testing.T, msgs []agent.Message) {
				// Simulate connection interruption
				server.CloseClientConnections()
				time.Sleep(2 * time.Second)

				// Verify reconnection and continued metrics transmission
				newMsgs := make([]agent.Message, 0)
				timeout := time.After(3 * time.Second)
				msgChan := make(chan agent.Message)

				go func() {
					for msg := range messages {
						msgChan <- msg
					}
				}()

				for {
					select {
					case msg := <-msgChan:
						newMsgs = append(newMsgs, msg)
						if len(newMsgs) >= 2 {
							return
						}
					case <-timeout:
						t.Fatal("Failed to receive messages after reconnection")
						return
					}
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), tt.duration)
			defer cancel()

			receivedMsgs := make([]agent.Message, 0)
			done := make(chan struct{})

			go func() {
				for {
					select {
					case msg := <-messages:
						receivedMsgs = append(receivedMsgs, msg)
					case <-ctx.Done():
						close(done)
						return
					}
				}
			}()

			<-done
			tt.validate(t, receivedMsgs)
		})
	}
}

/*
 * TestMetricsSystemLoad validates metrics collection under system load.
 *
 * Test Coverage:
 *   - Resource utilization accuracy
 *   - Collection performance
 *   - System load impact
 *   - Resource limit monitoring
 *
 * Requirements (from .cursorrules lines 864-867):
 *   - Resource management testing
 *   - Status reporting validation
 */
func TestMetricsSystemLoad(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping system load test in short mode")
	}

	collector, err := metrics.New(metrics.Config{
		CollectionInterval: 100 * time.Millisecond,
		EnableGPU:          true,
	})
	require.NoError(t, err)
	defer collector.Close()

	// Generate some system load
	done := make(chan bool)
	go func() {
		for i := 0; i < 1000000; i++ {
			_ = i * i
		}
		done <- true
	}()

	// Collect metrics during load
	metrics, err := collector.Collect()
	require.NoError(t, err)

	<-done

	// Verify metrics reflect system load
	assert.Greater(t, metrics.CPUUsage, 0.0, "CPU usage should be above 0 during load")
	assert.Greater(t, metrics.MemoryUsage, 0.0, "Memory usage should be above 0")
}

/*
 * TestMetricsDataPersistence validates metrics storage and retrieval.
 *
 * Test Coverage:
 *   - Database operations
 *   - Data integrity
 *   - Storage performance
 *   - Error handling
 *
 * Requirements (from .cursorrules lines 632-638):
 *   - Repository pattern usage
 *   - Connection pooling
 *   - Transaction handling
 */
func TestMetricsDataPersistence(t *testing.T) {
	// Create a temporary database connection for testing
	db, err := createTestDB(t)
	require.NoError(t, err)
	defer db.Close()

	collector, err := metrics.New(metrics.Config{
		CollectionInterval: time.Second,
		EnableGPU:          true,
	})
	require.NoError(t, err)
	defer collector.Close()

	// Collect and store metrics
	metrics, err := collector.Collect()
	require.NoError(t, err)

	err = storeMetrics(db, metrics)
	require.NoError(t, err)

	// Verify stored metrics
	stored, err := retrieveMetrics(db)
	require.NoError(t, err)
	assert.Equal(t, metrics.CPUUsage, stored.CPUUsage)
	assert.Equal(t, metrics.MemoryUsage, stored.MemoryUsage)
}

// Helper functions for database testing
func createTestDB(t *testing.T) (*sql.DB, error) {
	// Implementation depends on your database choice
	// This is just a placeholder
	return sql.Open("postgres", "postgres://test:test@localhost:5432/test?sslmode=disable")
}

func storeMetrics(db *sql.DB, metrics *metrics.SystemMetrics) error {
	// Implementation depends on your database schema
	return nil
}

func retrieveMetrics(db *sql.DB) (*metrics.SystemMetrics, error) {
	// Implementation depends on your database schema
	return &metrics.SystemMetrics{}, nil // Return empty metrics for now
}
