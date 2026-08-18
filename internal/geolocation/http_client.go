package geolocation

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/HamedFazaely/zx909-gw/internal/config"
)

// HTTPClient POSTs radio observations to a configurable geolocation URL.
//
// Request body (JSON):
//
//	{
//	  "wifi":  [{"mac": "aa:bb:cc:dd:ee:ff", "rssi": -65}],
//	  "cells": [{"mcc": 432, "mnc": 11, "lac": 1, "cell_id": 2, "signal": -90}]
//	}
//
// Expected response (JSON):
//
//	{"latitude": 35.6892, "longitude": 51.3890, "accuracy": 50}
//
// Put a thin adapter in front of vendor APIs (Google, MLS, Unwired Labs, …)
// if their wire format differs.
type HTTPClient struct {
	url     string
	apiKey  string
	client  *http.Client
	enabled bool
}

// NewClient returns a geolocation Client from config.
// When disabled or URL is empty, returns Disabled.
func NewClient(cfg config.GeolocationConfig) Client {
	if !cfg.Enabled || cfg.URL == "" {
		return Disabled{}
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	return &HTTPClient{
		url:     cfg.URL,
		apiKey:  cfg.APIKey,
		enabled: true,
		client:  &http.Client{Timeout: timeout},
	}
}

func (h *HTTPClient) Enabled() bool { return h.enabled }

func (h *HTTPClient) Locate(ctx context.Context, req Request) (*Result, error) {
	if len(req.Wifi) == 0 && len(req.Cells) == 0 {
		return nil, fmt.Errorf("geolocation: empty wifi and cells")
	}

	body, err := json.Marshal(httpRequestFrom(req))
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, h.url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if h.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+h.apiKey)
	}

	resp, err := h.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("geolocation HTTP %d: %s", resp.StatusCode, truncate(string(respBody), 200))
	}

	var out httpResponse
	if err := json.Unmarshal(respBody, &out); err != nil {
		return nil, fmt.Errorf("geolocation decode: %w", err)
	}
	if out.Latitude == 0 && out.Longitude == 0 {
		return nil, fmt.Errorf("geolocation: zero coordinates in response")
	}
	return &Result{
		Latitude:  out.Latitude,
		Longitude: out.Longitude,
		Accuracy:  out.Accuracy,
	}, nil
}

type httpRequest struct {
	Wifi  []httpWifi  `json:"wifi"`
	Cells []httpCell  `json:"cells"`
}

type httpWifi struct {
	MAC  string `json:"mac"`
	RSSI int    `json:"rssi"`
}

type httpCell struct {
	MCC    int   `json:"mcc"`
	MNC    int   `json:"mnc"`
	LAC    int   `json:"lac"`
	CellID int64 `json:"cell_id"`
	Signal int   `json:"signal"`
}

type httpResponse struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Accuracy  float64 `json:"accuracy"`
}

func httpRequestFrom(req Request) httpRequest {
	out := httpRequest{
		Wifi:  make([]httpWifi, 0, len(req.Wifi)),
		Cells: make([]httpCell, 0, len(req.Cells)),
	}
	for _, w := range req.Wifi {
		out.Wifi = append(out.Wifi, httpWifi{MAC: w.MAC, RSSI: w.RSSI})
	}
	for _, c := range req.Cells {
		out.Cells = append(out.Cells, httpCell{
			MCC: c.MCC, MNC: c.MNC, LAC: c.LAC, CellID: c.CellID, Signal: c.Signal,
		})
	}
	return out
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
