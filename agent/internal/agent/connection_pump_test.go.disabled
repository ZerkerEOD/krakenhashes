package agent

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/ZerkerEOD/krakenhashes/agent/internal/mocks"
	filesync "github.com/ZerkerEOD/krakenhashes/agent/internal/sync"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConnection_ReadPump(t *testing.T) {
	tests := []struct {
		name            string
		setupMock       func(*mocks.MockWebSocketConn)
		setupConnection func(*Connection)
		expectedCalls   func(*testing.T, *Connection)
	}{
		{
			name: "handle heartbeat message",
			setupMock: func(mockWS *mocks.MockWebSocketConn) {
				// Send heartbeat message
				msg := WSMessage{
					Type:    "heartbeat",
					AgentID: 123,
				}
				data, _ := json.Marshal(msg)
				mockWS.ReadMessages <- data
			},
			setupConnection: func(conn *Connection) {
				// Connection should handle heartbeat
			},
			expectedCalls: func(t *testing.T, conn *Connection) {
				// Heartbeat should update last ping time
				assert.WithinDuration(t, time.Now(), conn.lastPingTime, 2*time.Second)
			},
		},
		{
			name: "handle file sync request",
			setupMock: func(mockWS *mocks.MockWebSocketConn) {
				// Send file sync message
				msg := WSMessage{
					Type:    "file_sync",
					AgentID: 123,
					Data: map[string]interface{}{
						"file_type": "wordlist",
						"file_path": "test.txt",
					},
				}
				data, _ := json.Marshal(msg)
				mockWS.ReadMessages <- data
			},
			setupConnection: func(conn *Connection) {
				// Setup sync manager mock
				syncMgr := conn.syncManager.(*mocks.MockSyncManager)
				syncMgr.ProcessSyncCommandFunc = func(cmd interface{}) error {
					return nil
				}
			},
			expectedCalls: func(t *testing.T, conn *Connection) {
				// Sync manager should be called
				syncMgr := conn.syncManager.(*mocks.MockSyncManager)
				assert.Equal(t, 1, syncMgr.ProcessSyncCommandCalls)
			},
		},
		{
			name: "handle invalid JSON",
			setupMock: func(mockWS *mocks.MockWebSocketConn) {
				// Send invalid JSON
				mockWS.ReadMessages <- []byte("{invalid json")
			},
			setupConnection: func(conn *Connection) {},
			expectedCalls: func(t *testing.T, conn *Connection) {
				// Should continue without crashing
			},
		},
		{
			name: "handle read error",
			setupMock: func(mockWS *mocks.MockWebSocketConn) {
				mockWS.ReadError = errors.New("read error")
			},
			setupConnection: func(conn *Connection) {},
			expectedCalls: func(t *testing.T, conn *Connection) {
				// Should handle error gracefully
			},
		},
		{
			name: "handle close message",
			setupMock: func(mockWS *mocks.MockWebSocketConn) {
				mockWS.ReadError = websocket.ErrCloseSent
			},
			setupConnection: func(conn *Connection) {},
			expectedCalls: func(t *testing.T, conn *Connection) {
				// Should exit gracefully
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock WebSocket
			mockWS := mocks.NewMockWebSocketConn()
			tt.setupMock(mockWS)

			// Create connection
			conn := &Connection{
				ws:              mockWS,
				syncManager:     mocks.NewMockSyncManager(),
				hardwareMonitor: mocks.NewMockHardwareMonitor(),
				sendChan:        make(chan []byte, 10),
				stopChan:        make(chan struct{}),
			}
			tt.setupConnection(conn)

			// Run read pump in goroutine
			done := make(chan bool)
			go func() {
				conn.readPump()
				done <- true
			}()

			// Give it time to process
			time.Sleep(50 * time.Millisecond)

			// Close mock to stop pump
			mockWS.Close()

			// Wait for completion
			select {
			case <-done:
				// Good
			case <-time.After(1 * time.Second):
				t.Error("Read pump did not exit in time")
			}

			// Verify expectations
			tt.expectedCalls(t, conn)
		})
	}
}

func TestConnection_WritePump(t *testing.T) {
	tests := []struct {
		name            string
		setupMock       func(*mocks.MockWebSocketConn)
		setupConnection func(*Connection)
		sendMessages    []WSMessage
		expectedWrites  int
	}{
		{
			name: "send single message",
			setupMock: func(mockWS *mocks.MockWebSocketConn) {
				// No special setup
			},
			setupConnection: func(conn *Connection) {},
			sendMessages: []WSMessage{
				{
					Type:    "test",
					AgentID: 123,
				},
			},
			expectedWrites: 1,
		},
		{
			name: "send multiple messages",
			setupMock: func(mockWS *mocks.MockWebSocketConn) {
				// No special setup
			},
			setupConnection: func(conn *Connection) {},
			sendMessages: []WSMessage{
				{Type: "test1", AgentID: 123},
				{Type: "test2", AgentID: 123},
				{Type: "test3", AgentID: 123},
			},
			expectedWrites: 3,
		},
		{
			name: "handle write error",
			setupMock: func(mockWS *mocks.MockWebSocketConn) {
				mockWS.WriteError = errors.New("write error")
			},
			setupConnection: func(conn *Connection) {},
			sendMessages: []WSMessage{
				{Type: "test", AgentID: 123},
			},
			expectedWrites: 0,
		},
		{
			name: "handle heartbeat ticker",
			setupMock: func(mockWS *mocks.MockWebSocketConn) {},
			setupConnection: func(conn *Connection) {
				// Set short heartbeat interval for testing
				conn.lastPingTime = time.Now().Add(-2 * time.Minute)
			},
			sendMessages:   []WSMessage{},
			expectedWrites: 0, // Heartbeat will be sent
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock WebSocket
			mockWS := mocks.NewMockWebSocketConn()
			tt.setupMock(mockWS)

			// Create connection
			conn := &Connection{
				ws:       mockWS,
				sendChan: make(chan []byte, 10),
				stopChan: make(chan struct{}),
			}
			tt.setupConnection(conn)

			// Run write pump in goroutine
			done := make(chan bool)
			go func() {
				conn.writePump()
				done <- true
			}()

			// Send messages
			for _, msg := range tt.sendMessages {
				data, _ := json.Marshal(msg)
				conn.sendChan <- data
			}

			// Give it time to process
			time.Sleep(50 * time.Millisecond)

			// Stop pump
			close(conn.stopChan)

			// Wait for completion
			select {
			case <-done:
				// Good
			case <-time.After(1 * time.Second):
				t.Error("Write pump did not exit in time")
			}

			// Count writes
			writeCount := 0
			for {
				select {
				case <-mockWS.WriteMessages:
					writeCount++
				default:
					goto done
				}
			}
		done:

			// Verify write count
			if tt.expectedWrites > 0 {
				assert.GreaterOrEqual(t, writeCount, tt.expectedWrites)
			}
		})
	}
}

func TestConnection_HandleFileSyncAsync(t *testing.T) {
	tests := []struct {
		name          string
		fileType      string
		filePath      string
		setupSync     func(*mocks.MockSyncManager)
		expectedError bool
	}{
		{
			name:     "successful file sync",
			fileType: "wordlist",
			filePath: "test.txt",
			setupSync: func(m *mocks.MockSyncManager) {
				m.SyncFileFunc = func(fileType, filePath string) error {
					return nil
				}
			},
			expectedError: false,
		},
		{
			name:     "file sync error",
			fileType: "wordlist",
			filePath: "test.txt",
			setupSync: func(m *mocks.MockSyncManager) {
				m.SyncFileFunc = func(fileType, filePath string) error {
					return errors.New("sync failed")
				}
			},
			expectedError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mocks
			syncMgr := mocks.NewMockSyncManager()
			tt.setupSync(syncMgr)

			conn := &Connection{
				syncManager: syncMgr,
				sendChan:    make(chan []byte, 10),
			}

			// Call async handler
			conn.handleFileSyncAsync(tt.fileType, tt.filePath)

			// Give it time to process
			time.Sleep(50 * time.Millisecond)

			// Verify sync was called
			assert.Equal(t, 1, syncMgr.SyncFileCalls)

			// Check for response message
			select {
			case msg := <-conn.sendChan:
				var wsMsg WSMessage
				err := json.Unmarshal(msg, &wsMsg)
				require.NoError(t, err)

				if tt.expectedError {
					assert.Equal(t, "file_sync_error", wsMsg.Type)
				} else {
					assert.Equal(t, "file_sync_complete", wsMsg.Type)
				}
			default:
				t.Error("Expected response message")
			}
		})
	}
}

func TestConnection_MessageQueueing(t *testing.T) {
	t.Run("safeSendMessage", func(t *testing.T) {
		conn := &Connection{
			sendChan: make(chan []byte, 2), // Small buffer
		}

		// Fill the channel
		conn.sendChan <- []byte("msg1")
		conn.sendChan <- []byte("msg2")

		// This should not block
		done := make(chan bool)
		go func() {
			conn.safeSendMessage([]byte("msg3"))
			done <- true
		}()

		select {
		case <-done:
			// Good, didn't block
		case <-time.After(100 * time.Millisecond):
			t.Error("safeSendMessage blocked")
		}
	})
}

func TestConnection_ChannelReinitialization(t *testing.T) {
	conn := &Connection{}

	// Initial channels are nil
	assert.Nil(t, conn.sendChan)

	// Reinitialize
	conn.reinitializeChannels()

	// Channels should be created
	assert.NotNil(t, conn.sendChan)
	assert.NotNil(t, conn.stopChan)

	// Old channels should be closed and new ones created
	oldSend := conn.sendChan
	oldStop := conn.stopChan

	conn.reinitializeChannels()

	assert.NotEqual(t, oldSend, conn.sendChan)
	assert.NotEqual(t, oldStop, conn.stopChan)
}

func TestConnection_BinaryArchiveExtraction(t *testing.T) {
	tests := []struct {
		name        string
		setupSync   func(*mocks.MockSyncManager)
		expectCalls bool
	}{
		{
			name: "extract binary archives",
			setupSync: func(m *mocks.MockSyncManager) {
				// Return some binary files
				m.GetFileListFunc = func(fileType string) ([]filesync.FileInfo, error) {
					if fileType == "binary" {
						return []filesync.FileInfo{
							{Name: "hashcat.7z", Size: 1000},
							{Name: "john.7z", Size: 2000},
						}, nil
					}
					return []filesync.FileInfo{}, nil
				}
			},
			expectCalls: true,
		},
		{
			name: "no binary files",
			setupSync: func(m *mocks.MockSyncManager) {
				m.GetFileListFunc = func(fileType string) ([]filesync.FileInfo, error) {
					return []filesync.FileInfo{}, nil
				}
			},
			expectCalls: false,
		},
		{
			name: "error getting file list",
			setupSync: func(m *mocks.MockSyncManager) {
				m.GetFileListFunc = func(fileType string) ([]filesync.FileInfo, error) {
					return nil, errors.New("failed to get files")
				}
			},
			expectCalls: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			syncMgr := mocks.NewMockSyncManager()
			tt.setupSync(syncMgr)

			conn := &Connection{
				syncManager: syncMgr,
			}

			conn.checkAndExtractBinaryArchives()

			// Verify calls
			assert.Equal(t, 1, syncMgr.GetFileListCalls)
		})
	}
}