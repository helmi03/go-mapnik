package maptiles

import (
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"strings"
	"time"
)

type TileCoord struct {
	X, Y, Zoom uint64
	Tms        bool
	Layer      string
	Scale      string
	Url        string
	Format     string
}

type TileFetchResult struct {
	Coord TileCoord
	Blob  []byte
}

type TileFetchRequest struct {
	Coord   TileCoord
	OutChan chan<- TileFetchResult
}

func (c *TileCoord) setTMS(tms bool) {
	if c.Tms != tms {
		c.Y = (1 << c.Zoom) - c.Y - 1
		c.Tms = tms
	}
}

func NewTileRendererChan(stylesheet string) chan<- TileFetchRequest {
	c := make(chan TileFetchRequest)

	go func(requestChan <-chan TileFetchRequest) {
		var err error
		t := NewTileRenderer(stylesheet)
		for request := range requestChan {
			go func(request TileFetchRequest, t *TileRenderer) {
				result := TileFetchResult{request.Coord, nil}
				result.Blob, err = t.RenderTile(request.Coord)
				if err != nil {
					// log.Println("Error while rendering", request.Coord, ":", err.Error())
					result.Blob = nil
				}
				request.OutChan <- result
			}(request, t)
		}
	}(c)

	return c
}

// Renders images as Web Mercator tiles
type TileRenderer struct {
	m        string
	no_retry int
}

func NewTileRenderer(stylesheet string) *TileRenderer {
	t := new(TileRenderer)
	var err error
	if err != nil {
		log.Fatal(err)
	}
	t.no_retry = 3

	return t
}

func (t *TileRenderer) RenderTile(c TileCoord) ([]byte, error) {
	c.setTMS(false)
	return t.RenderTileZXY(c.Zoom, c.X, c.Y, c.Scale, c.Layer, c.Url, c.Format)
}

// Render a tile with coordinates in Google tile format.
// Most upper left tile is always 0,0. Method is not thread-safe,
// so wrap with a mutex when accessing the same renderer by multiple
// threads or setup multiple goroutines and communicate with channels,
// see NewTileRendererChan.
func (t *TileRenderer) RenderTileZXY(zoom, x, y uint64, scale, layer, url, format string) ([]byte, error) {
	tile_url := strings.Replace(url, "{z}", fmt.Sprintf("%d", zoom), 1)
	tile_url = strings.Replace(tile_url, "{x}", fmt.Sprintf("%d", x), 1)
	tile_url = strings.Replace(tile_url, "{y}", fmt.Sprintf("%d%s", y, scale), 1)
	tile_url = strings.Replace(tile_url, "{layer}", layer, 1)
	if format == "jpg" {
		format = "jpeg"
	}
	tile_url = strings.Replace(tile_url, "{format}", format, 1)
	// If sql error in datasource, Mapbox Studio resp.StatusCode=404
	var resp, err = http.Get(tile_url)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 && resp.StatusCode <= 499 {
		return nil, fmt.Errorf("%d", http.StatusNotFound)
	}
	// Only retry when "Error: Timed out after 5000ms" -- Assume Mapbox Studio
	// processing tile in background and we can retry request later
	// If reach this 5000ms error = candidate to refactor datasource
	if t.no_retry > 0 && resp.StatusCode >= 500 && resp.StatusCode <= 599 {
		ms := 500 * time.Millisecond
		// log.Printf("Retry #%d, wait %d ms", t.no_retry, ms/time.Millisecond)
		time.Sleep(ms)
		t.no_retry = t.no_retry - 1
		return t.RenderTileZXY(zoom, x, y, scale, layer, url, format)
	}
	if t.no_retry < 1 {
		// log.Println("Giveup")
		return nil, fmt.Errorf("Timeout from tile server")
	}

	defer resp.Body.Close()
	return ioutil.ReadAll(resp.Body)
}
