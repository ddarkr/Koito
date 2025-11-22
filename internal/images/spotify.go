package images

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/gabehf/koito/internal/cfg"
	"github.com/gabehf/koito/internal/logger"
	"github.com/gabehf/koito/internal/utils"
	"github.com/gabehf/koito/queue"
	"github.com/gabehf/koito/romanizer"
	"github.com/zmb3/spotify/v2"
)

type SpotifyClient struct {
	client       *spotify.Client
	url          string
	userAgent    string
	requestQueue *queue.RequestQueue
}

const (
	spotifyBaseUrl = "https://api.spotify.com/v1"
)

func NewSpotifyClient() *SpotifyClient {
	// For now, create a basic client - authentication will be added later
	client := spotify.New(http.DefaultClient)

	ret := new(SpotifyClient)
	ret.client = client
	ret.url = spotifyBaseUrl
	ret.userAgent = cfg.UserAgent()
	ret.requestQueue = queue.NewRequestQueue(5, 5)
	return ret
}

func (c *SpotifyClient) Shutdown() {
	c.requestQueue.Shutdown()
}


func (c *SpotifyClient) searchEntity(ctx context.Context, query string, searchType spotify.SearchType) (*spotify.SearchResult, error) {
	l := logger.FromContext(ctx)
	l.Debug().Msgf("Searching Spotify for: %s", query)

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