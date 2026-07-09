package ha

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

type APIClient struct {
	baseURL    string
	token      string
	httpClient *http.Client
	polled     map[string]func(entityID string, value float64)
	mu         sync.Mutex
	done       chan struct{}
}

type HAStateResponse struct {
	State       string                 `json:"state"`
	EntityID    string                 `json:"entity_id"`
	LastChanged string                 `json:"last_changed"`
	Attributes  map[string]interface{} `json:"attributes"`
}

func NewAPIClient(baseURL, token string) *APIClient {
	return &APIClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		token:      token,
		httpClient: &http.Client{Timeout: 10 * time.Second},
		polled:     make(map[string]func(entityID string, value float64)),
		done:       make(chan struct{}),
	}
}

func (a *APIClient) Start() {
	if a.baseURL == "" || a.token == "" {
		return
	}
	log.Printf("HA API: polling entity states from %s", a.baseURL)
	go a.loop()
}

func (a *APIClient) Stop() {
	close(a.done)
}

func (a *APIClient) UpdateConfig(baseURL, token string) {
	a.mu.Lock()
	a.baseURL = strings.TrimRight(baseURL, "/")
	a.token = token
	a.mu.Unlock()

	select {
	case <-a.done:
	default:
		close(a.done)
	}
	a.done = make(chan struct{})

	if a.baseURL != "" && a.token != "" {
		go a.loop()
	}
}

func (a *APIClient) Watch(entityID string, handler func(entityID string, value float64)) {
	a.mu.Lock()
	a.polled[entityID] = handler
	a.mu.Unlock()
}

func (a *APIClient) loop() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	a.pollAll()

	for {
		select {
		case <-a.done:
			return
		case <-ticker.C:
			a.pollAll()
		}
	}
}

func (a *APIClient) pollAll() {
	a.mu.Lock()
	entities := make([]string, 0, len(a.polled))
	for entityID := range a.polled {
		entities = append(entities, entityID)
	}
	handlers := make(map[string]func(entityID string, value float64), len(a.polled))
	for k, v := range a.polled {
		handlers[k] = v
	}
	a.mu.Unlock()

	for _, entityID := range entities {
		handler := handlers[entityID]
		value, err := a.fetchEntityState(entityID)
		if err != nil {
			log.Printf("HA API: failed to fetch %s: %v", entityID, err)
			continue
		}
		if value != nil {
			handler(entityID, *value)
		}
	}
}

func (a *APIClient) fetchEntityState(entityID string) (*float64, error) {
	u := fmt.Sprintf("%s/api/states/%s", a.baseURL, entityID)
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+a.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var stateResp HAStateResponse
	if err := json.NewDecoder(resp.Body).Decode(&stateResp); err != nil {
		return nil, err
	}

	val, err := strconv.ParseFloat(stateResp.State, 64)
	if err != nil {
		return nil, nil
	}
	return &val, nil
}

func (a *APIClient) FetchEntityState(entityID string) (*float64, error) {
	return a.fetchEntityState(entityID)
}

func (a *APIClient) GetEntityState(entityID string) (string, error) {
	u := fmt.Sprintf("%s/api/states/%s", a.baseURL, entityID)
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+a.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var stateResp HAStateResponse
	if err := json.NewDecoder(resp.Body).Decode(&stateResp); err != nil {
		return "", err
	}
	return strings.ToLower(stateResp.State), nil
}

func (a *APIClient) CallService(domain, service string, entityID string) error {
	u := fmt.Sprintf("%s/api/services/%s/%s", a.baseURL, domain, service)
	payload := map[string]interface{}{
		"entity_id": entityID,
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequest(http.MethodPost, u, strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+a.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HA service call %s/%s for %s: HTTP %d: %s", domain, service, entityID, resp.StatusCode, string(body))
	}
	return nil
}
