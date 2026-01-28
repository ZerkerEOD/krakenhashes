package services

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ZerkerEOD/krakenhashes/backend/internal/models"
	"github.com/ZerkerEOD/krakenhashes/backend/pkg/debug"
)

// WebhookPlatform represents a detected webhook platform
type WebhookPlatform string

const (
	PlatformGeneric WebhookPlatform = "generic"
	PlatformDiscord WebhookPlatform = "discord"
	PlatformSlack   WebhookPlatform = "slack"
	PlatformTeams   WebhookPlatform = "teams"
)

// NotificationWebhookService handles sending webhook notifications
type NotificationWebhookService struct {
	httpClient *http.Client
}

// NewNotificationWebhookService creates a new webhook service
func NewNotificationWebhookService() *NotificationWebhookService {
	return &NotificationWebhookService{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// DetectWebhookPlatform determines the webhook platform from URL
func DetectWebhookPlatform(url string) WebhookPlatform {
	switch {
	case strings.Contains(url, "discord.com/api/webhooks") || strings.Contains(url, "discordapp.com/api/webhooks"):
		return PlatformDiscord
	case strings.Contains(url, "hooks.slack.com"):
		return PlatformSlack
	case strings.Contains(url, "webhook.office.com") || strings.Contains(url, "outlook.office.com"):
		return PlatformTeams
	default:
		return PlatformGeneric
	}
}

// FormatPayloadForPlatform formats the payload for the detected platform
func FormatPayloadForPlatform(platform WebhookPlatform, payload *models.WebhookPayload) ([]byte, error) {
	switch platform {
	case PlatformDiscord:
		return formatDiscordPayload(payload)
	case PlatformSlack:
		return formatSlackPayload(payload)
	case PlatformTeams:
		return formatTeamsPayload(payload)
	default:
		return json.Marshal(payload)
	}
}

// formatDiscordPayload formats payload for Discord webhooks
func formatDiscordPayload(payload *models.WebhookPayload) ([]byte, error) {
	title, _ := payload.Data["title"].(string)
	message, _ := payload.Data["message"].(string)
	username, _ := payload.Data["username"].(string)
	userEmail, _ := payload.Data["user_email"].(string)

	// If no title/message, use event name
	if message == "" {
		message = fmt.Sprintf("Event: %s", payload.Event)
	}

	discord := map[string]interface{}{}

	// Use embeds for richer notifications if we have both title and message
	if title != "" && message != "" {
		// Determine color based on event type
		color := 5814783 // Default blue
		if strings.Contains(payload.Event, "error") || strings.Contains(payload.Event, "failed") {
			color = 15158332 // Red
		} else if strings.Contains(payload.Event, "completed") || strings.Contains(payload.Event, "success") {
			color = 3066993 // Green
		} else if strings.Contains(payload.Event, "warning") {
			color = 16776960 // Yellow
		}

		embed := map[string]interface{}{
			"title":       title,
			"description": message,
			"color":       color,
			"timestamp":   time.Unix(payload.Timestamp, 0).Format(time.RFC3339),
			"footer": map[string]string{
				"text": "KrakenHashes Notification",
			},
		}

		// Add user info as fields if available
		if username != "" || userEmail != "" {
			fields := []map[string]interface{}{}
			if username != "" {
				fields = append(fields, map[string]interface{}{
					"name":   "User",
					"value":  username,
					"inline": true,
				})
			}
			if userEmail != "" {
				fields = append(fields, map[string]interface{}{
					"name":   "Email",
					"value":  userEmail,
					"inline": true,
				})
			}
			embed["fields"] = fields
		}

		discord["embeds"] = []map[string]interface{}{embed}
	} else {
		// Simple message format - prepend username if available
		if username != "" {
			message = fmt.Sprintf("**User:** %s\n%s", username, message)
		}
		discord["content"] = message
	}

	return json.Marshal(discord)
}

// formatSlackPayload formats payload for Slack webhooks
func formatSlackPayload(payload *models.WebhookPayload) ([]byte, error) {
	title, _ := payload.Data["title"].(string)
	message, _ := payload.Data["message"].(string)
	username, _ := payload.Data["username"].(string)
	userEmail, _ := payload.Data["user_email"].(string)

	// If no message, use event name
	if message == "" {
		message = fmt.Sprintf("Event: %s", payload.Event)
	}

	slack := map[string]interface{}{
		"text": message,
	}

	// Add blocks for richer notifications if we have a title
	if title != "" {
		blocks := []map[string]interface{}{
			{
				"type": "header",
				"text": map[string]string{
					"type": "plain_text",
					"text": title,
				},
			},
			{
				"type": "section",
				"text": map[string]string{
					"type": "mrkdwn",
					"text": message,
				},
			},
		}

		// Add user info section if available
		if username != "" || userEmail != "" {
			userInfo := ""
			if username != "" {
				userInfo = fmt.Sprintf("*User:* %s", username)
			}
			if userEmail != "" {
				if userInfo != "" {
					userInfo += " | "
				}
				userInfo += fmt.Sprintf("*Email:* %s", userEmail)
			}
			blocks = append(blocks, map[string]interface{}{
				"type": "section",
				"text": map[string]string{
					"type": "mrkdwn",
					"text": userInfo,
				},
			})
		}

		// Add context with event and time
		blocks = append(blocks, map[string]interface{}{
			"type": "context",
			"elements": []map[string]string{
				{
					"type": "mrkdwn",
					"text": fmt.Sprintf("*Event:* %s | *Time:* <!date^%d^{date_short_pretty} {time}|%s>",
						payload.Event,
						payload.Timestamp,
						time.Unix(payload.Timestamp, 0).Format(time.RFC3339)),
				},
			},
		})

		slack["blocks"] = blocks
	}

	return json.Marshal(slack)
}

// formatTeamsPayload formats payload for Microsoft Teams webhooks
func formatTeamsPayload(payload *models.WebhookPayload) ([]byte, error) {
	title, _ := payload.Data["title"].(string)
	message, _ := payload.Data["message"].(string)
	username, _ := payload.Data["username"].(string)
	userEmail, _ := payload.Data["user_email"].(string)

	// If no title/message, use event name
	if title == "" {
		title = "KrakenHashes Notification"
	}
	if message == "" {
		message = fmt.Sprintf("Event: %s", payload.Event)
	}

	// Determine theme color based on event type
	themeColor := "0076D7" // Default blue
	if strings.Contains(payload.Event, "error") || strings.Contains(payload.Event, "failed") {
		themeColor = "E74856" // Red
	} else if strings.Contains(payload.Event, "completed") || strings.Contains(payload.Event, "success") {
		themeColor = "2DC76D" // Green
	} else if strings.Contains(payload.Event, "warning") {
		themeColor = "FFB900" // Yellow
	}

	// Build facts array with event, time, and user info
	facts := []map[string]string{
		{
			"name":  "Event",
			"value": payload.Event,
		},
		{
			"name":  "Time",
			"value": time.Unix(payload.Timestamp, 0).Format(time.RFC1123),
		},
	}

	// Add user info as facts if available
	if username != "" {
		facts = append(facts, map[string]string{
			"name":  "User",
			"value": username,
		})
	}
	if userEmail != "" {
		facts = append(facts, map[string]string{
			"name":  "Email",
			"value": userEmail,
		})
	}

	teams := map[string]interface{}{
		"@type":      "MessageCard",
		"@context":   "http://schema.org/extensions",
		"themeColor": themeColor,
		"summary":    title,
		"sections": []map[string]interface{}{
			{
				"activityTitle": title,
				"text":          message,
				"facts":         facts,
			},
		},
	}

	return json.Marshal(teams)
}

// Send delivers a webhook payload with retry logic
func (s *NotificationWebhookService) Send(
	ctx context.Context,
	url string,
	payload *models.WebhookPayload,
	secret *string,
	customHeaders models.JSONMap,
	retryCount int,
	timeoutSeconds int,
) error {
	// Detect platform and format payload accordingly
	platform := DetectWebhookPlatform(url)
	body, err := FormatPayloadForPlatform(platform, payload)
	if err != nil {
		return fmt.Errorf("failed to format payload for platform %s: %w", platform, err)
	}

	debug.Log("Sending webhook", map[string]interface{}{
		"url":      url,
		"platform": string(platform),
	})

	// Set timeout if specified
	client := s.httpClient
	if timeoutSeconds > 0 && timeoutSeconds != 30 {
		client = &http.Client{
			Timeout: time.Duration(timeoutSeconds) * time.Second,
		}
	}

	// Retry logic with exponential backoff
	var lastErr error
	for attempt := 0; attempt <= retryCount; attempt++ {
		if attempt > 0 {
			// Exponential backoff: 1s, 2s, 4s, etc.
			backoff := time.Duration(1<<uint(attempt-1)) * time.Second
			if backoff > 30*time.Second {
				backoff = 30 * time.Second
			}

			debug.Log("Retrying webhook", map[string]interface{}{
				"url":     url,
				"attempt": attempt + 1,
				"backoff": backoff.String(),
			})

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
		}

		err := s.sendRequest(ctx, client, url, body, secret, customHeaders)
		if err == nil {
			return nil
		}
		lastErr = err

		debug.Warning("Webhook delivery attempt failed", map[string]interface{}{
			"url":     url,
			"attempt": attempt + 1,
			"error":   err.Error(),
		})
	}

	return fmt.Errorf("webhook delivery failed after %d attempts: %w", retryCount+1, lastErr)
}

// sendRequest sends a single webhook request
func (s *NotificationWebhookService) sendRequest(
	ctx context.Context,
	client *http.Client,
	url string,
	body []byte,
	secret *string,
	customHeaders models.JSONMap,
) error {
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Set standard headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "KrakenHashes-Webhook/1.0")
	req.Header.Set("X-KrakenHashes-Delivery", fmt.Sprintf("%d", time.Now().Unix()))

	// Add signature if secret is configured
	if secret != nil && *secret != "" {
		signature := s.computeSignature(body, *secret)
		req.Header.Set("X-Signature-256", "sha256="+signature)
		req.Header.Set("X-Hub-Signature-256", "sha256="+signature) // GitHub-style for compatibility
	}

	// Add custom headers
	if customHeaders != nil {
		for key, value := range customHeaders {
			if strValue, ok := value.(string); ok {
				req.Header.Set(key, strValue)
			}
		}
	}

	// Send request
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read response body for error messages
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))

	// Check response status
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned status %d: %s", resp.StatusCode, string(respBody))
	}

	debug.Log("Webhook delivered successfully", map[string]interface{}{
		"url":    url,
		"status": resp.StatusCode,
	})

	return nil
}

// computeSignature computes HMAC-SHA256 signature
func (s *NotificationWebhookService) computeSignature(payload []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}

// VerifySignature verifies a webhook signature (for incoming webhooks if needed)
func (s *NotificationWebhookService) VerifySignature(payload []byte, signature string, secret string) bool {
	expected := s.computeSignature(payload, secret)

	// Remove "sha256=" prefix if present
	if len(signature) > 7 && signature[:7] == "sha256=" {
		signature = signature[7:]
	}

	return hmac.Equal([]byte(expected), []byte(signature))
}

// TestWebhook sends a test notification to a webhook URL
func (s *NotificationWebhookService) TestWebhook(ctx context.Context, url string, secret *string, customHeaders models.JSONMap) error {
	// Detect platform for logging
	platform := DetectWebhookPlatform(url)
	debug.Log("Testing webhook", map[string]interface{}{
		"url":      url,
		"platform": string(platform),
	})

	payload := &models.WebhookPayload{
		Event:     "test",
		Timestamp: time.Now().Unix(),
		Data: map[string]interface{}{
			"title":   "KrakenHashes Webhook Test",
			"message": "This is a test notification from KrakenHashes. If you see this message, your webhook is configured correctly!",
			"test":    true,
		},
	}

	return s.Send(ctx, url, payload, secret, customHeaders, 0, 10) // No retries for test, 10 second timeout
}
