package commoncrawl

import (
	"bufio"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type CatalogEntry struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Client struct {
	httpClient *http.Client
}

func NewClient() *Client {
	return &Client{
		httpClient: &http.Client{
			Timeout: 30 * time.Second, // Allow some time for downloading wat.paths.gz
		},
	}
}

func (c *Client) FetchCatalog(ctx context.Context) ([]CatalogEntry, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", "https://index.commoncrawl.org/collinfo.json", nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status fetching catalog: %d", resp.StatusCode)
	}

	var catalog []CatalogEntry
	if err := json.NewDecoder(resp.Body).Decode(&catalog); err != nil {
		return nil, err
	}

	return catalog, nil
}

func (c *Client) FetchWATPaths(ctx context.Context, crawlID string) ([]string, error) {
	url := fmt.Sprintf("https://data.commoncrawl.org/crawl-data/%s/wat.paths.gz", crawlID)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	// For the WAT paths downloading we might want a longer context or timeout,
	// but we'll respect the passed context.
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status fetching wat paths: %d", resp.StatusCode)
	}

	gzReader, err := gzip.NewReader(resp.Body)
	if err != nil {
		return nil, err
	}
	defer gzReader.Close()

	var paths []string
	scanner := bufio.NewScanner(gzReader)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			paths = append(paths, line)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return paths, nil
}
