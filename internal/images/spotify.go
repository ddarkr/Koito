package images

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gabehf/koito/internal/cfg"
	"github.com/gabehf/koito/internal/logger"
	"github.com/gabehf/koito/internal/utils"
	"github.com/gabehf/koito/queue"
	"github.com/gabehf/koito/romanizer"
	"github.com/zmb3/spotify/v2"
)

// authTransport adds Authorization header to HTTP requests
type authTransport struct {
	client *SpotifyClient
}

func (t *authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.client.accessToken != "" {
		req.Header.Set("Authorization", "Bearer "+t.client.accessToken)
	}
	return http.DefaultTransport.RoundTrip(req)
}

type SpotifyClient struct {
	client       *spotify.Client
	httpClient   *http.Client
	url          string
	userAgent    string
	requestQueue *queue.RequestQueue
	accessToken  string
	tokenExpiry  time.Time
}

const (
	spotifyBaseUrl = "https://api.spotify.com/v1"
)

func NewSpotifyClient() *SpotifyClient {
	ret := new(SpotifyClient)
	ret.url = spotifyBaseUrl
	ret.userAgent = cfg.UserAgent()
	ret.requestQueue = queue.NewRequestQueue(5, 5)

	// Create authenticated HTTP client
	ret.httpClient = &http.Client{
		Transport: &authTransport{client: ret},
	}

	// Authenticate with Spotify
	err := ret.authenticate()
	if err != nil {
		// Log error but don't fail - client will work without auth for now
		// This allows the system to continue working even if Spotify auth fails
	}

	// Create Spotify client with authenticated HTTP client
	ret.client = spotify.New(ret.httpClient)

	return ret
}

func (c *SpotifyClient) authenticate() error {
	clientID := cfg.SpotifyClientId()
	clientSecret := cfg.SpotifyClientSecret()

	if clientID == "" || clientSecret == "" {
		return fmt.Errorf("Spotify client ID or secret not configured")
	}

	// Debug log client ID (without secret for security)
	logger.Get().Debug().Str("client_id", clientID).Msg("Attempting Spotify authentication")

	// Prepare the request
	data := url.Values{}
	data.Set("grant_type", "client_credentials")

	req, err := http.NewRequest("POST", "https://accounts.spotify.com/api/token", strings.NewReader(data.Encode()))
	if err != nil {
		return fmt.Errorf("failed to create auth request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	auth := base64.StdEncoding.EncodeToString([]byte(clientID + ":" + clientSecret))
	req.Header.Set("Authorization", "Basic "+auth)

	// Make the request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to authenticate with Spotify: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// Read response body for error details
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("Spotify auth failed with status: %d, body: %s", resp.StatusCode, string(body))
	}

	// Parse response
	var tokenResp struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		ExpiresIn   int    `json:"expires_in"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return fmt.Errorf("failed to parse auth response: %w", err)
	}

	// Store token
	c.accessToken = tokenResp.AccessToken
	c.tokenExpiry = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)

	return nil
}

func (c *SpotifyClient) ensureToken(ctx context.Context) error {
	if c.accessToken == "" || time.Now().After(c.tokenExpiry.Add(-5*time.Minute)) {
		// Token is missing or will expire in less than 5 minutes
		return c.authenticate()
	}
	return nil
}

func (c *SpotifyClient) Shutdown() {
	c.requestQueue.Shutdown()
}


func (c *SpotifyClient) searchEntity(ctx context.Context, query string, searchType spotify.SearchType) (*spotify.SearchResult, error) {
	l := logger.FromContext(ctx)
	l.Debug().Msgf("Searching Spotify for: %s", query)

	// Ensure we have a valid token
	if err := c.ensureToken(ctx); err != nil {
		l.Err(err).Msg("Failed to ensure valid Spotify token")
		return nil, fmt.Errorf("searchEntity: %w", err)
	}

	results, err := c.client.Search(ctx, query, searchType)
	if err != nil {
		l.Err(err).Msg("Spotify search failed")
		return nil, fmt.Errorf("searchEntity: %w", err)
	}

	return results, nil
}

func (c *SpotifyClient) GetArtistImages(ctx context.Context, aliases []string) (string, error) {
	l := logger.FromContext(ctx)
	aliasesUniq := utils.UniqueIgnoringCase(aliases)

	// First try romanized names with exact quotes
	for _, a := range aliasesUniq {
		romanized := romanizer.Romanize(a)
		if romanized != "" {
			results, err := c.searchEntity(ctx, fmt.Sprintf("artist:\"%s\"", romanized), spotify.SearchTypeArtist)
			if err != nil {
				return "", fmt.Errorf("GetArtistImages: %w", err)
			}
			if results.Artists != nil && len(results.Artists.Artists) > 0 {
				for _, artist := range results.Artists.Artists {
					if strings.EqualFold(artist.Name, romanized) || strings.EqualFold(artist.Name, a) || strings.Contains(strings.ToLower(artist.Name), strings.ToLower(a)) {
						if len(artist.Images) > 0 {
							img := artist.Images[0].URL
							l.Debug().Msgf("Found artist images for %s (romanized: %s): %v", a, romanized, img)
							return img, nil
						}
					}
				}
			}
		}
	}

	// Then try original names with exact quotes
	for _, a := range aliasesUniq {
		results, err := c.searchEntity(ctx, fmt.Sprintf("artist:\"%s\"", a), spotify.SearchTypeArtist)
		if err != nil {
			return "", fmt.Errorf("GetArtistImages: %w", err)
		}
		if results.Artists != nil && len(results.Artists.Artists) > 0 {
			for _, artist := range results.Artists.Artists {
				if strings.EqualFold(artist.Name, a) || strings.Contains(strings.ToLower(artist.Name), strings.ToLower(a)) {
					if len(artist.Images) > 0 {
						img := artist.Images[0].URL
						l.Debug().Msgf("Found artist images for %s: %v", a, img)
						return img, nil
					}
				}
			}
		}
	}

	// Try without quotes for broader matching
	for _, a := range aliasesUniq {
		results, err := c.searchEntity(ctx, fmt.Sprintf("artist:%s", a), spotify.SearchTypeArtist)
		if err != nil {
			return "", fmt.Errorf("GetArtistImages: %w", err)
		}
		if results.Artists != nil && len(results.Artists.Artists) > 0 {
			for _, artist := range results.Artists.Artists {
				if strings.EqualFold(artist.Name, a) || strings.Contains(strings.ToLower(artist.Name), strings.ToLower(a)) {
					if len(artist.Images) > 0 {
						img := artist.Images[0].URL
						l.Debug().Msgf("Found artist images for %s (no quotes): %v", a, img)
						return img, nil
					}
				}
			}
		}
	}

	// Try combining aliases with OR for multiple aliases
	if len(aliasesUniq) > 1 {
		queryParts := make([]string, len(aliasesUniq))
		for i, a := range aliasesUniq {
			queryParts[i] = fmt.Sprintf("artist:\"%s\"", a)
		}
		combinedQuery := strings.Join(queryParts, " OR ")
		results, err := c.searchEntity(ctx, combinedQuery, spotify.SearchTypeArtist)
		if err != nil {
			return "", fmt.Errorf("GetArtistImages: %w", err)
		}
		if results.Artists != nil && len(results.Artists.Artists) > 0 {
			for _, artist := range results.Artists.Artists {
				for _, a := range aliasesUniq {
					if strings.EqualFold(artist.Name, a) || strings.Contains(strings.ToLower(artist.Name), strings.ToLower(a)) {
						if len(artist.Images) > 0 {
							img := artist.Images[0].URL
							l.Debug().Msgf("Found artist images for combined aliases %v: %v", aliasesUniq, img)
							return img, nil
						}
					}
				}
			}
		}
	}

	return "", errors.New("GetArtistImages: artist image not found")
}

func (c *SpotifyClient) GetAlbumImages(ctx context.Context, artists []string, album string) (string, error) {
	l := logger.FromContext(ctx)
	l.Debug().Msgf("Finding album image for %s from artist(s) %v", album, artists)

	artistsUniq := utils.UniqueIgnoringCase(artists)

	// Try to find artist + album match for all artists with more query combinations
	for _, artist := range artistsUniq {
		romanizedArtist := romanizer.Romanize(artist)
		romanizedAlbum := romanizer.Romanize(album)

		queries := []string{}

		// Original combinations
		if romanizedAlbum != "" {
			queries = append(queries, fmt.Sprintf("artist:\"%s\" album:\"%s\"", artist, romanizedAlbum))
		}
		if romanizedArtist != "" {
			queries = append(queries, fmt.Sprintf("artist:\"%s\" album:\"%s\"", romanizedArtist, album))
			if romanizedAlbum != "" {
				queries = append(queries, fmt.Sprintf("artist:\"%s\" album:\"%s\"", romanizedArtist, romanizedAlbum))
			}
		}
		queries = append(queries, fmt.Sprintf("artist:\"%s\" album:\"%s\"", artist, album))

		// Additional combinations without quotes for broader matching
		queries = append(queries, fmt.Sprintf("artist:%s album:\"%s\"", artist, album))
		if romanizedAlbum != "" {
			queries = append(queries, fmt.Sprintf("artist:%s album:\"%s\"", artist, romanizedAlbum))
		}
		if romanizedArtist != "" {
			queries = append(queries, fmt.Sprintf("artist:%s album:\"%s\"", romanizedArtist, album))
			if romanizedAlbum != "" {
				queries = append(queries, fmt.Sprintf("artist:%s album:\"%s\"", romanizedArtist, romanizedAlbum))
			}
		}

		for _, query := range queries {
			results, err := c.searchEntity(ctx, query, spotify.SearchTypeAlbum)
			if err != nil {
				return "", fmt.Errorf("GetAlbumImages: %w", err)
			}
			if results.Albums != nil && len(results.Albums.Albums) > 0 {
				for _, alb := range results.Albums.Albums {
					if strings.EqualFold(alb.Name, album) || strings.Contains(strings.ToLower(alb.Name), strings.ToLower(album)) {
						if len(alb.Images) > 0 {
							img := alb.Images[0].URL
							l.Debug().Msgf("Found album images for %s: %v", album, img)
							return img, nil
						}
					}
				}
			}
		}
	}

	// Try combining multiple artists with OR
	if len(artistsUniq) > 1 {
		artistQueryParts := make([]string, len(artistsUniq))
		for i, artist := range artistsUniq {
			artistQueryParts[i] = fmt.Sprintf("artist:\"%s\"", artist)
		}
		combinedArtistQuery := strings.Join(artistQueryParts, " OR ")
		queries := []string{
			fmt.Sprintf("(%s) album:\"%s\"", combinedArtistQuery, album),
		}
		romanizedAlbum := romanizer.Romanize(album)
		if romanizedAlbum != "" {
			queries = append(queries, fmt.Sprintf("(%s) album:\"%s\"", combinedArtistQuery, romanizedAlbum))
		}

		for _, query := range queries {
			results, err := c.searchEntity(ctx, query, spotify.SearchTypeAlbum)
			if err != nil {
				return "", fmt.Errorf("GetAlbumImages: %w", err)
			}
			if results.Albums != nil && len(results.Albums.Albums) > 0 {
				for _, alb := range results.Albums.Albums {
					if strings.EqualFold(alb.Name, album) || strings.Contains(strings.ToLower(alb.Name), strings.ToLower(album)) {
						if len(alb.Images) > 0 {
							img := alb.Images[0].URL
							l.Debug().Msgf("Found album images for %s with combined artists: %v", album, img)
							return img, nil
						}
					}
				}
			}
		}
	}

	// If none found, try album title only with more variations
	queries := []string{}
	romanizedAlbum := romanizer.Romanize(album)
	if romanizedAlbum != "" {
		queries = append(queries, fmt.Sprintf("album:\"%s\"", romanizedAlbum))
		queries = append(queries, fmt.Sprintf("album:%s", romanizedAlbum))
	}
	queries = append(queries, fmt.Sprintf("album:\"%s\"", album))
	queries = append(queries, fmt.Sprintf("album:%s", album))

	for _, query := range queries {
		results, err := c.searchEntity(ctx, query, spotify.SearchTypeAlbum)
		if err != nil {
			return "", fmt.Errorf("GetAlbumImages: %w", err)
		}
		if results.Albums != nil && len(results.Albums.Albums) > 0 {
			for _, alb := range results.Albums.Albums {
				if strings.EqualFold(alb.Name, album) || strings.Contains(strings.ToLower(alb.Name), strings.ToLower(album)) {
					if len(alb.Images) > 0 {
						img := alb.Images[0].URL
						l.Debug().Msgf("Found album images for %s (album only): %v", album, img)
						return img, nil
					}
				}
			}
		}
	}

	return "", errors.New("GetAlbumImages: album image not found")
}