package alerts

import (
	"crypto/rand"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/robinelvin/bedwetter/config"
)

type NtfyClient struct {
	cfg    *config.Config
	client *http.Client
}

func NewNtfyClient(cfg *config.Config) *NtfyClient {
	return &NtfyClient{
		cfg:    cfg,
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

func (n *NtfyClient) Send(level, title, message string) {
	ncfg := n.cfg.Ntfy
	if !ncfg.Enabled || ncfg.Server == "" || ncfg.UUID == "" {
		return
	}

	switch level {
	case "info":
		if !ncfg.AlertInfo {
			return
		}
	case "warn":
		if !ncfg.AlertWarn {
			return
		}
	case "alarm":
		if !ncfg.AlertAlarm {
			return
		}
	default:
		return
	}

	topic := ncfg.UUID
	url := strings.TrimRight(ncfg.Server, "/") + "/" + topic

	req, err := http.NewRequest("POST", url, strings.NewReader(message))
	if err != nil {
		log.Printf("ntfy: failed to create request: %v", err)
		return
	}

	req.Header.Set("Title", title)
	req.Header.Set("Priority", ntfyPriority(level))
	req.Header.Set("Tags", ntfyTags(level))

	if ncfg.Token != "" {
		req.Header.Set("Authorization", "Bearer "+ncfg.Token)
	}

	resp, err := n.client.Do(req)
	if err != nil {
		log.Printf("ntfy: failed to send: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		log.Printf("ntfy: server returned status %d", resp.StatusCode)
		return
	}
	log.Printf("ntfy: sent %s notification: %s", level, title)
}

func ntfyPriority(level string) string {
	switch level {
	case "alarm":
		return "5"
	case "warn":
		return "4"
	default:
		return "3"
	}
}

func ntfyTags(level string) string {
	switch level {
	case "alarm":
		return "rotating_light,alarm"
	case "warn":
		return "warning"
	default:
		return "droplet,information_source"
	}
}

func GenerateNtfyUUID() string {
	var buf [16]byte
	rand.Read(buf[:])
	buf[6] = (buf[6] & 0x0f) | 0x40 // version 4
	buf[8] = (buf[8] & 0x3f) | 0x80 // variant 10xx
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		buf[0:4], buf[4:6], buf[6:8], buf[8:10], buf[10:16])
}
